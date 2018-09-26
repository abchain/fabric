@echo off
for /l %%i in (1,1,%1) do call:body %%i
exit /b

:body
setlocal
set PEERADDRBASE=7055
set PEERLOCALADDRBASE=7051
set EVENTADDRBASE=7053
set FILEPATHBASE=%appdata%\hyperledger01

set /a ADDRPORT=%PEERADDRBASE%+%1*100
set /a LOCADDRPORT=%PEERLOCALADDRBASE%+%1*100
set /a EVENTADDRPORT=%EVENTADDRBASE%+%1*100
set CORE_PEER_LISTENADDRESS=127.0.0.1:%ADDRPORT%
set CORE_PEER_ADDRESS=%CORE_PEER_LISTENADDRESS%
set CORE_PEER_LOCALADDR=127.0.0.1:%LOCADDRPORT%
set CORE_PEER_ID=billgates_%1
set CORE_PEER_VALIDATOR_EVENTS_ADDRESS=127.0.0.1:%EVENTADDRPORT%
set CORE_PEER_FILESYSTEMPATH=%FILEPATHBASE%\txnet%1
timeout 1
start txnetwork
endlocal