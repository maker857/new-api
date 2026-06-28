$env:PORT='3000'
$env:SQLITE_PATH='D:\log new-api\new-api\local-test.db?_busy_timeout=30000'
$env:SESSION_SECRET='local-test-secret'
$env:GENERATE_DEFAULT_TOKEN='true'
$env:DEBUG='true'
$env:ERROR_LOG_ENABLED='true'
Set-Location 'D:\log new-api\new-api'
go run .
