
@echo off
for %%i in (*.proto) do call :setlist %%i

protoc -I=%1\src\github.com\abchain\fabric\protos -I=. --go_out=plugins=grpc:. %LIST%

echo DONE: %LIST%

exit /b

:setlist
set LIST=%LIST% %~nx1
