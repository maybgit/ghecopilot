$rootPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location -Path $rootPath

if (!(Test-Path -Path .env)) {
    Copy-Item .\.env.example .\.env -ErrorAction SilentlyContinue
}

# Get-ChildItem logs -Directory -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force


if ($env:COMPUTERNAME -eq "DESKTOP-FG53PVS" -and $env:USERNAME -eq "mayb") {
    $c = (Get-Content .env) -replace '^UPSTREAM_API_KEY=.*$', 'UPSTREAM_API_KEY='
    $c | Out-File .\.env.example -Encoding utf8
}

Stop-Process -Name ghecopilot -Force -ErrorAction SilentlyContinue
go build -o ghecopilot.exe

if ($?) {
    $env:GIN_MODE="release"
    .\ghecopilot.exe
}

