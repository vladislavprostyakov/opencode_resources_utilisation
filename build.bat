@echo off
cd /d "%~dp0"
go build -o opencode-resources.exe . 2> build.log
echo done > build.done
