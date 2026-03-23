$initReq = '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
$toolsReq = '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
$createProject = '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mos_create_project","arguments":{"name":"test-project","description":"Test project for Hippocampus"}}}'
$rememberReq = '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mos_remember","arguments":{"content":"TimescaleDB uses hypertables for time-series data. Compression policies reduce storage by 90%.","importance":0.9,"tags":["timescaledb","architecture","database"],"session_id":"test-session-1"}}}'
$recallReq = '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mos_recall","arguments":{"query":"How does TimescaleDB handle compression?","limit":5}}}'

$messages = @($initReq, $toolsReq, $createProject, $rememberReq, $recallReq)

$allInput = ""
foreach ($msg in $messages) {
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($msg)
    $header = "Content-Length: $($bytes.Length)`r`n`r`n"
    $allInput += $header + $msg
}

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = "$PSScriptRoot\bin\hippocampus.exe"
$psi.Arguments = "-config config.json -migrations migrations"
$psi.WorkingDirectory = $PSScriptRoot
$psi.UseShellExecute = $false
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.CreateNoWindow = $true

$proc = [System.Diagnostics.Process]::Start($psi)

$proc.StandardInput.Write($allInput)
$proc.StandardInput.Close()

Start-Sleep -Seconds 8

$stdout = $proc.StandardOutput.ReadToEnd()
$stderr = $proc.StandardError.ReadToEnd()

Write-Host "=== STDERR (logs) ==="
Write-Host $stderr
Write-Host ""
Write-Host "=== STDOUT (MCP responses) ==="
Write-Host $stdout

if (-not $proc.HasExited) {
    $proc.Kill()
}
