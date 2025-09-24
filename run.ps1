# Run this from project root in PowerShell
go env -w GO111MODULE=on
setx GOPROXY "https://proxy.golang.org,direct"
go clean -modcache
go mod tidy
go run main.go

