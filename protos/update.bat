
@echo off
for %%i in (*.proto) do call :setlist %%i

protoc --go_out=plugins=grpc:%GOPATH%\src %LIST%

echo DONE: %LIST%

exit /b

:setlist
set LIST=%LIST% %~nx1
