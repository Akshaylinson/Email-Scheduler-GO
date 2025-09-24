package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	dbPath         = "db/scheduler.db"
	uploadsDir     = "uploads"
	workerCount    = 4
	taskQueueSize  = 1000
	defaultSMTPPort = "587"
)

var (
	db       *sql.DB
	taskQ    chan SendTask
	wg       sync.WaitGroup
)

type SendTask struct {
	SendID   string
	JobID    string
	Email    string
	Subject  string
	Body     string
}

// job JSON for schedule endpoint
type JobReq struct {
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	ScheduledAt string `json:"scheduled_at"` // RFC3339 or unix seconds (optional)
}

func main() {
	ensureDirs()
	initDB()
	startWorkers(workerCount)

	// start scheduler loop
	go schedulerLoop()

	r := mux.NewRouter()
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("templates/assets"))))

	r.HandleFunc("/", serveIndex).Methods("GET")
	r.HandleFunc("/upload", uploadCSVHandler).Methods("POST")
	r.HandleFunc("/schedule", scheduleJobHandler).Methods("POST")
	r.HandleFunc("/jobs", listJobsHandler).Methods("GET")
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request){ w.WriteHeader(200); w.Write([]byte("ok")) }).Methods("GET")

	addr := ":8080"
	log.Printf("starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func ensureDirs() {
	if err := os.MkdirAll("db", 0755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Fatalf("create uploads dir: %v", err)
	}
}

// ---------------- DB init ----------------

func initDB() {
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS subscribers (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		subject TEXT,
		body TEXT,
		scheduled_at INTEGER,
		status TEXT,
		created_at INTEGER NOT NULL,
		completed_at INTEGER
	);

	CREATE TABLE IF NOT EXISTS sends (
		id TEXT PRIMARY KEY,
		job_id TEXT,
		subscriber_id TEXT,
		email TEXT,
		status TEXT,
		attempts INTEGER DEFAULT 0,
		last_error TEXT,
		created_at INTEGER NOT NULL,
		sent_at INTEGER
	);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("migrate schema: %v", err)
	}
}

// ---------------- Handlers ----------------

// serveIndex serves the static HTML page (templates/index.html)
func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/index.html")
}

// uploadCSVHandler accepts multipart form file field "file" with CSV
// Each row: first column is email
func uploadCSVHandler(w http.ResponseWriter, r *http.Request) {
	// limit
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20) // 20MB
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "file too large or invalid form", 400)
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", 400)
		return
	}
	defer f.Close()

	// store uploaded file optionally
	dst := filepath.Join(uploadsDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), fh.Filename))
	out, err := os.Create(dst)
	if err == nil {
		io.Copy(out, io.NopCloser(io.LimitReader(f, 1<<30)))
		out.Close()
		// reopen for parsing
		f, _ = os.Open(dst)
		defer f.Close()
	} else {
		// if storing fails, reset the file reading (we still have the stream)
		// but proceed with the existing stream - ok to continue
	}

	reader := csv.NewReader(f)
	added := 0
	for {
		rec, err := reader.Read()
		if err == io.EOF { break }
		if err != nil { log.Println("csv read:", err); continue }
		if len(rec) == 0 { continue }
		email := strings.TrimSpace(rec[0])
		if email == "" { continue }
		id := uuid.New().String()
		_, err = db.Exec("INSERT INTO subscribers(id,email,created_at) VALUES(?,?,?)", id, email, time.Now().Unix())
		if err != nil {
			// duplicate or error — ignore duplicates
			continue
		}
		added++
	}
	w.Header().Set("Content-Type","application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"added": added})
}

// scheduleJobHandler: accept subject, body, scheduled_at
func scheduleJobHandler(w http.ResponseWriter, r *http.Request) {
	var req JobReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	subject := strings.TrimSpace(req.Subject)
	body := strings.TrimSpace(req.Body)
	if subject == "" || body == "" {
		http.Error(w, "subject and body required", 400)
		return
	}

	var ts int64
	if req.ScheduledAt == "" {
		ts = time.Now().Unix()
	} else {
		if t, err := time.Parse(time.RFC3339, req.ScheduledAt); err == nil {
			ts = t.Unix()
		} else if i, err := strconv.ParseInt(req.ScheduledAt,10,64); err == nil {
			ts = i
		} else {
			http.Error(w, "invalid scheduled_at", 400)
			return
		}
	}

	id := uuid.New().String()
	_, err := db.Exec("INSERT INTO jobs(id,subject,body,scheduled_at,status,created_at) VALUES(?,?,?,?,?,?)",
		id, subject, body, ts, "pending", time.Now().Unix())
	if err != nil {
		log.Println("insert job:", err)
		http.Error(w, "db error", 500)
		return
	}
	w.Header().Set("Content-Type","application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "scheduled_at": ts})
}

// listJobsHandler: returns jobs
func listJobsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id,subject,body,scheduled_at,status,created_at,completed_at FROM jobs ORDER BY created_at DESC")
	if err != nil { http.Error(w,"db error",500); return }
	defer rows.Close()
	type jobResp struct {
		ID string `json:"id"`
		Subject string `json:"subject"`
		Body string `json:"body"`
		ScheduledAt int64 `json:"scheduled_at"`
		Status string `json:"status"`
		CreatedAt int64 `json:"created_at"`
		CompletedAt *int64 `json:"completed_at"`
	}
	var out []jobResp
	for rows.Next() {
		var j jobResp
		var comp sql.NullInt64
		if err := rows.Scan(&j.ID,&j.Subject,&j.Body,&j.ScheduledAt,&j.Status,&j.CreatedAt,&comp); err==nil {
			if comp.Valid { v := comp.Int64; j.CompletedAt = &v }
			out = append(out, j)
		}
	}
	w.Header().Set("Content-Type","application/json")
	json.NewEncoder(w).Encode(out)
}

// ---------------- Scheduler + workers ----------------

func schedulerLoop() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		enqueueDueJobs()
	}
}

func enqueueDueJobs() {
	now := time.Now().Unix()
	rows, err := db.Query("SELECT id,subject,body FROM jobs WHERE status = ? AND scheduled_at <= ?", "pending", now)
	if err != nil {
		log.Println("enqueue query:", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, subject, body string
		if err := rows.Scan(&id,&subject,&body); err != nil { continue }
		// mark running
		if _, err := db.Exec("UPDATE jobs SET status = ? WHERE id = ?", "running", id); err != nil {
			log.Println("mark running:", err)
			continue
		}
		// dispatch
		go dispatchJob(id, subject, body)
	}
}

func dispatchJob(jobID, subject, body string) {
	// create sends for each subscriber and enqueue tasks
	rows, err := db.Query("SELECT id,email FROM subscribers")
	if err != nil {
		log.Println("dispatch subscribers:", err)
		_, _ = db.Exec("UPDATE jobs SET status = ?, completed_at = ? WHERE id = ?", "failed", time.Now().Unix(), jobID)
		return
	}
	defer rows.Close()

	var tasks []SendTask
	for rows.Next() {
		var sid, email string
		if err := rows.Scan(&sid,&email); err != nil { continue }
		sendID := uuid.New().String()
		_, err := db.Exec("INSERT INTO sends(id,job_id,subscriber_id,email,status,created_at,attempts) VALUES(?,?,?,?,?,?,0)", sendID, jobID, sid, email, "queued", time.Now().Unix())
		if err != nil { continue }
		tasks = append(tasks, SendTask{
			SendID: sendID,
			JobID: jobID,
			Email: email,
			Subject: subject,
			Body: body,
		})
	}

	for _, t := range tasks {
		taskQ <- t
	}

	// wait until sends for this job finish (simple polling)
	for {
		var remaining int
		_ = db.QueryRow("SELECT COUNT(1) FROM sends WHERE job_id = ? AND status IN ('queued','sending')", jobID).Scan(&remaining)
		if remaining == 0 { break }
		time.Sleep(1 * time.Second)
	}

	// finalize
	var failed int
	_ = db.QueryRow("SELECT COUNT(1) FROM sends WHERE job_id = ? AND status = ?", jobID, "failed").Scan(&failed)
	if failed > 0 {
		_, _ = db.Exec("UPDATE jobs SET status = ?, completed_at = ? WHERE id = ?", "completed_with_errors", time.Now().Unix(), jobID)
	} else {
		_, _ = db.Exec("UPDATE jobs SET status = ?, completed_at = ? WHERE id = ?", "completed", time.Now().Unix(), jobID)
	}
}

// workers

func startWorkers(n int) {
	taskQ = make(chan SendTask, taskQueueSize)
	for i:=0;i<n;i++ {
		wg.Add(1)
		go worker(i+1)
	}
}

func worker(idx int) {
	defer wg.Done()
	for t := range taskQ {
		// mark sending
		_, _ = db.Exec("UPDATE sends SET status = ?, attempts = attempts+1 WHERE id = ?", "sending", t.SendID)
		err := doSend(t)
		if err != nil {
			_, _ = db.Exec("UPDATE sends SET status = ?, last_error = ? WHERE id = ?", "failed", err.Error(), t.SendID)
			log.Printf("[Worker-%d] send failed job=%s email=%s err=%v", idx, t.JobID, t.Email, err)
			continue
		}
		_, _ = db.Exec("UPDATE sends SET status = ?, sent_at = ? WHERE id = ?", "sent", time.Now().Unix(), t.SendID)
		log.Printf("[Worker-%d] sent job=%s email=%s", idx, t.JobID, t.Email)
	}
}

// ---------------- Email sending ----------------

func doSend(t SendTask) error {
	// if SMTP env not set, mock send
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := os.Getenv("SMTP_PORT")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")
	if from == "" { from = "no-reply@example.com" }

	if smtpHost == "" {
		// mock
		log.Printf("[MOCK SEND] to=%s subject=%s bodyLen=%d", t.Email, t.Subject, len(t.Body))
		return nil
	}

	if smtpPort == "" { smtpPort = defaultSMTPPort }
	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	msg := buildMessage(from, t.Email, t.Subject, t.Body)
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	return smtp.SendMail(addr, auth, from, []string{t.Email}, []byte(msg))
}

func buildMessage(from, to, subject, body string) string {
	sb := &strings.Builder{}
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return sb.String()
}

