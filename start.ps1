$rootPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location -Path $rootPath

if (!(Test-Path -Path .env)) {
    Copy-Item .\.env.example .\.env -ErrorAction SilentlyContinue
}

# Get-ChildItem logs -Directory -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force

Stop-Process -Name ghecopilot -Force -ErrorAction SilentlyContinue

if ($env:CLIENTNAME -eq "DESKTOP-2DONPVA" -and $env:USERNAME -eq "mayb") {
    $c = (Get-Content .env) -replace '^UPSTREAM_API_KEY=.*$', 'UPSTREAM_API_KEY='
    $c | Out-File .\.env.example -Encoding utf8
}

go build -o ghecopilot.exe

if ($?) {
    $env:GIN_MODE="release"
    .\ghecopilot.exe
}

