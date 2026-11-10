$ErrorActionPreference = "Stop"
$env:GOROOT = "D:\go1.27.3.windows-amd64\go"
$env:PATH = "D:\go1.27.3.windows-amd64\go\bin;" + $env:PATH
Set-Location $PSScriptRoot
go build -ldflags="-H windowsgui -s -w" -o hd2-strain-lab.exe .
Write-Host "Built: $PSScriptRoot\hd2-strain-lab.exe"
