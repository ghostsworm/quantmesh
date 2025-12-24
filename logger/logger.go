package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	DEBUG LogLevel = iota // 调试信息（最详细）
	INFO                  // 一般信息（正常运行信息）
	WARN                  // 警告信息（需要注意但不影响运行）
	ERROR                 // 错误信息（需要关注的问题）
	FATAL                 // 致命错误（程序无法继续）
)

var (
	globalLevel LogLevel = INFO
	mu          sync.RWMutex
	
	// 文件日志相关
	fileLogger   *log.Logger
	logFile      *os.File
	currentDate  string
	fileMu       sync.Mutex
	logDir       = "log" // 日志文件夹
)

// String 返回日志级别的字符串表示
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel 解析日志级别字符串
func ParseLogLevel(level string) LogLevel {
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO // 默认INFO级别
	}
}

// SetLevel 设置全局日志级别
func SetLevel(level LogLevel) {
	mu.Lock()
	defer mu.Unlock()
	globalLevel = level
	
	// 如果设置为DEBUG级别，启用文件日志
	if level == DEBUG {
		initFileLogger()
	} else {
		closeFileLogger()
	}
}

// initFileLogger 初始化文件日志（当日志级别为DEBUG时）
func initFileLogger() {
	fileMu.Lock()
	defer fileMu.Unlock()
	
	// 如果已经初始化且日期相同，不需要重新初始化
	today := time.Now().Format("2006-01-02")
	if fileLogger != nil && currentDate == today {
		return
	}
	
	// 关闭旧文件
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
	
	// 创建log文件夹
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// 如果创建失败，只输出到控制台
		log.Printf("[WARN] 创建日志文件夹失败: %v，将只输出到控制台", err)
		return
	}
	
	// 创建日志文件（按日期命名）
	logFileName := filepath.Join(logDir, fmt.Sprintf("opensqt-%s.log", today))
	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// 如果打开失败，只输出到控制台
		log.Printf("[WARN] 打开日志文件失败: %v，将只输出到控制台", err)
		return
	}
	
	logFile = file
	currentDate = today
	// 创建文件日志器（不包含时间戳，因为标准log已经包含）
	fileLogger = log.New(file, "", 0)
	
	log.Printf("[INFO] 文件日志已启用，日志文件: %s", logFileName)
}

// closeFileLogger 关闭文件日志
func closeFileLogger() {
	fileMu.Lock()
	defer fileMu.Unlock()
	
	if logFile != nil {
		logFile.Close()
		logFile = nil
		fileLogger = nil
		currentDate = ""
	}
}

// checkAndRotateLog 检查并轮转日志文件（如果需要）
// 注意：调用此函数前必须已持有fileMu锁
func checkAndRotateLog() {
	today := time.Now().Format("2006-01-02")
	if currentDate != today {
		// 日期变化，重新初始化文件日志
		// 关闭旧文件
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
		
		// 创建log文件夹
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return
		}
		
		// 创建新的日志文件
		logFileName := filepath.Join(logDir, fmt.Sprintf("opensqt-%s.log", today))
		file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		
		logFile = file
		currentDate = today
		fileLogger = log.New(file, "", 0)
	}
}

// Close 关闭文件日志（程序退出时调用）
func Close() {
	closeFileLogger()
}

// GetLevel 获取全局日志级别
func GetLevel() LogLevel {
	mu.RLock()
	defer mu.RUnlock()
	return globalLevel
}

// shouldLog 判断是否应该输出日志
func shouldLog(level LogLevel) bool {
	return level >= globalLevel
}

// logf 内部日志输出函数
func logf(level LogLevel, format string, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	prefix := fmt.Sprintf("[%s] ", level.String())
	message := fmt.Sprintf(prefix+format, args...)
	
	// 输出到控制台（标准输出）
	log.Printf(prefix+format, args...)
	
	// 如果日志级别为DEBUG，同时写入文件
	if globalLevel == DEBUG {
		fileMu.Lock()
		// 检查是否需要轮转日志文件
		checkAndRotateLog()
		if fileLogger != nil {
			// 写入文件（包含时间戳）
			fileLogger.Printf("%s %s", time.Now().Format("2006/01/02 15:04:05"), message)
		}
		fileMu.Unlock()
	}
}

// logln 内部日志输出函数（无格式）
func logln(level LogLevel, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	prefix := fmt.Sprintf("[%s] ", level.String())
	message := fmt.Sprintln(append([]interface{}{prefix}, args...)...)
	
	// 输出到控制台（标准输出）
	log.Println(append([]interface{}{prefix}, args...)...)
	
	// 如果日志级别为DEBUG，同时写入文件
	if globalLevel == DEBUG {
		fileMu.Lock()
		// 检查是否需要轮转日志文件
		checkAndRotateLog()
		if fileLogger != nil {
			// 写入文件（包含时间戳，去掉末尾的换行符，因为Println会自动添加）
			fileLogger.Printf("%s %s", time.Now().Format("2006/01/02 15:04:05"), strings.TrimSuffix(message, "\n"))
		}
		fileMu.Unlock()
	}
}

// Debug 输出调试日志
func Debug(format string, args ...interface{}) {
	logf(DEBUG, format, args...)
}

// Debugln 输出调试日志（无格式）
func Debugln(args ...interface{}) {
	logln(DEBUG, args...)
}

// Info 输出一般信息日志
func Info(format string, args ...interface{}) {
	logf(INFO, format, args...)
}

// Infoln 输出一般信息日志（无格式）
func Infoln(args ...interface{}) {
	logln(INFO, args...)
}

// Warn 输出警告日志
func Warn(format string, args ...interface{}) {
	logf(WARN, format, args...)
}

// Warnln 输出警告日志（无格式）
func Warnln(args ...interface{}) {
	logln(WARN, args...)
}

// Error 输出错误日志
func Error(format string, args ...interface{}) {
	logf(ERROR, format, args...)
}

// Errorln 输出错误日志（无格式）
func Errorln(args ...interface{}) {
	logln(ERROR, args...)
}

// Fatal 输出致命错误日志并退出程序
func Fatal(format string, args ...interface{}) {
	logf(FATAL, format, args...)
	os.Exit(1)
}

// Fatalln 输出致命错误日志并退出程序（无格式）
func Fatalln(args ...interface{}) {
	logln(FATAL, args...)
	os.Exit(1)
}

// Fatalf 输出致命错误日志并退出程序（兼容标准库）
func Fatalf(format string, args ...interface{}) {
	Fatal(format, args...)
}

