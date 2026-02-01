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
		fmt.Println("用法: go run set_password.go <新密碼>")
		os.Exit(1)
	}

	newPassword := os.Args[1]
	username := "admin"

	// 數據库路径
	dataDir := "./data"
	dbPath := filepath.Join(dataDir, "auth.db")

	// 检查數據库文件是否存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Printf("錯误: 數據库文件不存在: %s\n", dbPath)
		os.Exit(1)
	}

	// 打开數據库
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Printf("錯误: 打开數據库失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// 生成密碼哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("錯误: 生成密碼哈希失败: %v\n", err)
		os.Exit(1)
	}

	// 更新密碼
	_, err = db.Exec(`
		INSERT INTO users (username, password_hash) 
		VALUES (?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = ?
	`, username, string(hash), string(hash))
	if err != nil {
		fmt.Printf("錯误: 更新密碼失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ 密碼已成功更新為: %s\n", newPassword)
	fmt.Printf("  用戶名: %s\n", username)
	fmt.Printf("  數據库: %s\n", dbPath)
}

