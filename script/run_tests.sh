#!/usr/bin/env bash
# 分布式锁系统测试运行脚本 (Linux/Mac)
# 确保脚本在Linux和Mac上都能正确执行

set -e  # 遇到错误立即退出

# 检查Go是否安装
if ! command -v go &> /dev/null; then
    echo "错误: 未找到Go命令，请先安装Go"
    exit 1
fi

echo "========================================"
echo "分布式锁系统 - 测试运行脚本"
echo "========================================"
echo ""

show_menu() {
    echo "请选择要运行的测试:"
    echo ""
    echo "1. 运行所有测试"
    echo "2. 运行服务端测试"
    echo "3. 运行客户端测试"
    echo "4. 运行服务端测试（详细输出）"
    echo "5. 运行客户端测试（详细输出）"
    echo "6. 运行特定测试用例"
    echo "7. 查看测试覆盖率"
    echo "8. 退出"
    echo ""
}

while true; do
    show_menu
    read -p "请输入选项 (1-8): " choice
    
    case $choice in
        1)
            echo ""
            echo "运行所有测试..."
            go test -v ./...
            ;;
        2)
            echo ""
            echo "运行服务端测试..."
            go test ./server
            ;;
        3)
            echo ""
            echo "运行客户端测试..."
            go test ./client
            ;;
        4)
            echo ""
            echo "运行服务端测试（详细输出）..."
            go test -v ./server
            ;;
        5)
            echo ""
            echo "运行客户端测试（详细输出）..."
            go test -v ./client
            ;;
        6)
            echo ""
            echo "可用的测试用例:"
            echo "- TestConcurrentPullOperations"
            echo "- TestPullSkipWhenRefCountNotZero"
            echo "- TestDeleteWithReferences"
            echo "- TestDeleteWithoutReferences"
            echo "- TestUpdateWithReferences"
            echo "- TestFIFOQueue"
            echo "- TestRetryMechanism"
            echo "- TestTimeout"
            echo ""
            read -p "请输入测试名称: " test_name
            read -p "请输入包名 (server/client): " package
            echo ""
            if [ -z "$test_name" ] || [ -z "$package" ]; then
                echo "错误: 测试名称和包名不能为空"
            else
                echo "运行测试: $test_name in $package"
                go test -v -run "$test_name" ./"$package"
            fi
            ;;
        7)
            echo ""
            echo "生成测试覆盖率报告..."
            go test -cover ./server
            go test -cover ./client
            echo ""
            echo "生成详细覆盖率报告..."
            go test -coverprofile=server_coverage.out ./server
            go test -coverprofile=client_coverage.out ./client
            if [ -f "server_coverage.out" ] && [ -f "client_coverage.out" ]; then
                echo "覆盖率报告已生成: server_coverage.out, client_coverage.out"
                echo "查看HTML报告: go tool cover -html=server_coverage.out"
            else
                echo "警告: 覆盖率报告生成失败"
            fi
            ;;
        8)
            echo ""
            echo "退出测试脚本"
            exit 0
            ;;
        *)
            echo "无效选项，请重新选择"
            ;;
    esac
    
    echo ""
    read -p "按Enter继续..."
done

