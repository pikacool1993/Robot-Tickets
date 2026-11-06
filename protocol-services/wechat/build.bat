@echo off

set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

go build -ldflags="-s -w" -trimpath -o main.exe main.go

echo Build Success
pause