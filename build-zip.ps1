$rootPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location -Path $rootPath

$zip = "ghecopilot_$(Get-Date -Format yy.M.d).zip"
Remove-Item $zip -ErrorAction SilentlyContinue
Compress-Archive .\ghecopilot.exe,.\.env.example $zip  -CompressionLevel Optimal