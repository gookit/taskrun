@echo off
cd /d D:\work\inhere\my-tools-dev\gookit2\kscript
set "GOTOOLCHAIN=go1.23.12"
echo == 1.23 build ==
go build ./... && echo BUILD_123_OK || echo BUILD_123_FAIL
echo == 1.23 test ==
go test -count=1 ./... && echo TEST_123_OK || echo TEST_123_FAIL
set "GOTOOLCHAIN="
echo == cross linux ==
set "GOOS=linux"
go build ./... && echo LINUX_OK || echo LINUX_FAIL
set "GOOS=darwin"
go build ./... && echo DARWIN_OK || echo DARWIN_FAIL
set "GOOS="
echo == done ==
