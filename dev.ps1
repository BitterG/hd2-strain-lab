$ErrorActionPreference = "Stop"
$env:GOROOT = "D:\go1.27.3.windows-amd64\go"
$env:PATH = "D:\go1.27.3.windows-amd64\go\bin;" + $env:PATH
Set-Location $PSScriptRoot
go run .
