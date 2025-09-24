.

📧 Email Scheduler (Go)

A lightweight Email Scheduling System built with Go, featuring:

📂 CSV subscriber import

⏰ Job scheduling with optional delayed execution

📨 Worker-based email sending (mock or real SMTP)

🗂 Persistent storage with SQLite

🖥 Bootstrap-powered web dashboard

🚀 Features

Subscriber Management

Upload subscribers via CSV (one email per line)

Automatically stored in SQLite

Job Scheduling

Create jobs with subject & body

Schedule immediately or at a specific time

Task Queue + Workers

Configurable worker pool

Parallel email delivery

Tracks per-send status (queued, sending, sent, failed)

Dashboard (Web UI)

Upload subscribers

Schedule newsletter jobs

Monitor job history & status

Mock or Real Sending

If SMTP is not configured → mock send (logs only)

If SMTP is configured → real email delivery

📂 Project Structure
email-scheduler/
├── db/                  # SQLite database file (scheduler.db)
├── scripts/             # Helper scripts
│   ├── run.ps1          # Windows runner
│   └── run.sh           # Linux / macOS runner
├── templates/
│   └── index.html       # Dashboard UI (Bootstrap)
├── uploads/             # Uploaded CSV files
├── go.mod               # Go module file
├── go.sum               # Dependency lockfile
├── main.go              # Main application server
└── README.md            # Documentation

⚙️ Requirements

Go 1.25+

No external DB needed (uses embedded SQLite)

Optional: SMTP credentials (for real sending)

🔧 Setup & Run
1. Clone the repository
git clone https://github.com/you/email-scheduler.git
cd email-scheduler

2. Install dependencies
go clean -modcache
go mod tidy

3. Run the application

Windows (PowerShell):

.\scripts\run.ps1


macOS / Linux:

./scripts/run.sh


Server starts at:
👉 http://localhost:8080

🌐 Usage
Upload Subscribers

Prepare a .csv file with one email per line:

alice@example.com
bob@example.com
carol@example.com


Upload via the dashboard UI.

Schedule a Job

Enter Subject and Body.

Optionally pick a date/time.

Submit → job is stored in DB and executed when due.

Monitor Jobs

Jobs and their statuses (pending, running, completed, failed) appear in the Jobs list.

📡 API Endpoints
Method	Endpoint	Description
POST	/upload	Upload CSV file of subscribers
POST	/schedule	Schedule a new job (JSON payload)
GET	/jobs	List all jobs with statuses
GET	/health	Health check (returns ok)
📬 SMTP Configuration (Optional)

By default, emails are mocked and logged.
To enable real SMTP sending, set environment variables:

export SMTP_HOST=smtp.gmail.com
export SMTP_PORT=587
export SMTP_USER=your-email@gmail.com
export SMTP_PASS=your-app-password
export SMTP_FROM=your-email@gmail.com


On Windows (PowerShell):

setx SMTP_HOST "smtp.gmail.com"
setx SMTP_PORT "587"
setx SMTP_USER "your-email@gmail.com"
setx SMTP_PASS "your-app-password"
setx SMTP_FROM "your-email@gmail.com"

🛠 Tech Stack

Backend: Go, Gorilla Mux, SQLite

Database: Embedded SQLite (via modernc.org/sqlite)

Frontend: HTML + Bootstrap 5 (single-page dashboard)

Queue: In-memory task channel with worker pool

📊 Roadmap / Future Enhancements

 Add authentication for dashboard access

 Email templates (HTML) support

 Retry with exponential backoff for failed sends

 Export job reports as CSV/PDF

 S3-compatible storage for subscriber lists

🤝 Contributing

Fork this repo

Create a feature branch (git checkout -b feature/foo)

Commit changes (git commit -m 'Add foo')

Push branch (git push origin feature/foo)

Open a Pull Request

📄 License

MIT License © 2025 [Your Name]

⚡ With this system, you can quickly manage subscriber lists, schedule newsletters, and handle email dispatch — all in pure Go.
