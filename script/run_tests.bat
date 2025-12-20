@echo off
chcp 65001 >nul 2>&1
REM Distributed Lock System Test Script (Windows)

echo ========================================
echo Distributed Lock System - Test Runner
echo ========================================
echo.

:menu
echo Please select test to run:
echo.
echo 1. Run all tests
echo 2. Run server tests
echo 3. Run client tests
echo 4. Run server tests (verbose)
echo 5. Run client tests (verbose)
echo 6. Run specific test case
echo 7. View test coverage
echo 8. Exit
echo.
set /p choice=Enter option 1-8:  

if "%choice%"=="1" goto all_tests
if "%choice%"=="2" goto server_tests
if "%choice%"=="3" goto client_tests
if "%choice%"=="4" goto server_tests_verbose
if "%choice%"=="5" goto client_tests_verbose
if "%choice%"=="6" goto specific_test
if "%choice%"=="7" goto coverage
if "%choice%"=="8" goto end
goto menu

:all_tests
echo.
echo Running all tests...
go test -v ./...
goto menu

:server_tests
echo.
echo Running server tests...
go test ./server
goto menu

:client_tests
echo.
echo Running client tests...
go test ./client
goto menu

:server_tests_verbose
echo.
echo Running server tests (verbose)...
go test -v ./server
goto menu

:client_tests_verbose
echo.
echo Running client tests (verbose)...
go test -v ./client
goto menu

:specific_test
echo.
echo Available test cases:
echo - TestConcurrentPullOperations
echo - TestPullSkipWhenRefCountNotZero
echo - TestDeleteWithReferences
echo - TestDeleteWithoutReferences
echo - TestUpdateWithReferences
echo - TestFIFOQueue
echo - TestRetryMechanism
echo - TestTimeout
echo.
set /p test_name=Enter test name: 
set /p package=Enter package name (server/client): 
echo.
echo Running test: %test_name% in %package%
go test -v -run %test_name% ./%package%
goto menu

:coverage
echo.
echo Generating test coverage report...
go test -cover ./server
go test -cover ./client
echo.
echo Generating detailed coverage report...
go test -coverprofile=server_coverage.out ./server
go test -coverprofile=client_coverage.out ./client
echo Coverage reports generated: server_coverage.out, client_coverage.out
goto menu

:end
echo.
echo Exiting test script
pause

