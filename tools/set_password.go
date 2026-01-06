package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: go run set_password.go <新密码>")
		os.Exit(1)
	}

	newPassword := os.Args[1]
	username := "admin"

	// 数据库路径
	dataDir := "./data"
	dbPath := filepath.Join(dataDir, "auth.db")

	// 检查数据库文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Printf("错误: 数据库文件不存在: %s\n", dbPath)
		os.Exit(1)
	}

	// 打开数据库
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Printf("错误: 打开数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// 生成密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("错误: 生成密码哈希失败: %v\n", err)
		os.Exit(1)
	}

	// 更新密码
	_, err = db.Exec(`
		INSERT INTO users (username, password_hash) 
		VALUES (?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = ?
	`, username, string(hash), string(hash))
	if err != nil {
		fmt.Printf("错误: 更新密码失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ 密码已成功更新为: %s\n", newPassword)
	fmt.Printf("  用户名: %s\n", username)
	fmt.Printf("  数据库: %s\n", dbPath)
}

