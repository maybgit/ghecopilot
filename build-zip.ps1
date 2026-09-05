$rootPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location -Path $rootPath

$zip = "ghecopilot_v$(Get-Date -Format yy.M.d).zip"
Remove-Item $zip -ErrorAction SilentlyContinue
Compress-Archive .\ghecopilot.exe,.\.env.example $zip  -CompressionLevel Optimal