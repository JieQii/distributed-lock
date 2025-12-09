@echo off
REM 分布式锁系统测试运行脚本 (Windows)

echo ========================================
echo 分布式锁系统 - 测试运行脚本
echo ========================================
echo.

:menu
echo 请选择要运行的测试:
echo.
echo 1. 运行所有测试
echo 2. 运行服务端测试
echo 3. 运行客户端测试
echo 4. 运行服务端测试（详细输出）
echo 5. 运行客户端测试（详细输出）
echo 6. 运行特定测试用例
echo 7. 查看测试覆盖率
echo 8. 退出
echo.
set /p choice=请输入选项 (1-8): 

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
echo 运行所有测试...
go test -v ./...
goto menu

:server_tests
echo.
echo 运行服务端测试...
go test ./server
goto menu

:client_tests
echo.
echo 运行客户端测试...
go test ./client
goto menu

:server_tests_verbose
echo.
echo 运行服务端测试（详细输出）...
go test -v ./server
goto menu

:client_tests_verbose
echo.
echo 运行客户端测试（详细输出）...
go test -v ./client
goto menu

:specific_test
echo.
echo 可用的测试用例:
echo - TestConcurrentPullOperations
echo - TestPullSkipWhenRefCountNotZero
echo - TestDeleteWithReferences
echo - TestDeleteWithoutReferences
echo - TestUpdateWithReferences
echo - TestFIFOQueue
echo - TestRetryMechanism
echo - TestTimeout
echo.
set /p test_name=请输入测试名称: 
set /p package=请输入包名 (server/client): 
echo.
echo 运行测试: %test_name% in %package%
go test -v -run %test_name% ./%package%
goto menu

:coverage
echo.
echo 生成测试覆盖率报告...
go test -cover ./server
go test -cover ./client
echo.
echo 生成详细覆盖率报告...
go test -coverprofile=server_coverage.out ./server
go test -coverprofile=client_coverage.out ./client
echo 覆盖率报告已生成: server_coverage.out, client_coverage.out
goto menu

:end
echo.
echo 退出测试脚本
pause

