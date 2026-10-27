param(
    [string]$HostIP = "0.0.0.0",
    [int]$Port = 43001
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location "$scriptDir\ai-services"

Write-Host "Starting VAANI Whisper ASR service on ${HostIP}:${Port}..." -ForegroundColor Cyan
& "C:\Program Files\Python310\python.exe" -m uvicorn asr.asr:app --host $HostIP --port $Port --reload
