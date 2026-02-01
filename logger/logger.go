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
	DEBUG LogLevel = iota // 調試信息（最详细）
	INFO                  // 一般信息（正常运行信息）
	WARN                  // 警告信息（需要注意但不影响运行）
	ERROR                 // 錯误信息（需要关注的问题）
	FATAL                 // 致命錯误（程序無法继续）
)

var (
	globalLevel LogLevel = INFO
	mu          sync.RWMutex

	// 应用日志文件相关
	fileLogger  *log.Logger
	logFile     *os.File
	currentDate string
	fileMu      sync.Mutex
	logDir      = "logs" // 日志文件夹

	// Web 日志文件相关
	webFileLogger  *log.Logger
	webLogFile     *os.File
	webCurrentDate string
	webFileMu      sync.Mutex

	// 時区相关
	globalLocation *time.Location = time.Local
	locationMu     sync.RWMutex

	// SQLite 日志存儲（通過函數指針避免循环依赖）
	logStorageWriter func(level, message string)
	logStorageMu     sync.RWMutex

	// 日志语言配置
	logLanguage string = "zh-CN"
	langMu      sync.RWMutex

	// i18n 翻譯函數（避免循环依赖）
	translateFunc func(key string, data ...interface{}) string
	translateMu   sync.RWMutex
)

// builderPool 字符串構建器對象池，用於複用 strings.Builder，减少記憶體分配
var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// maxLogMessageLength 最大日志消息长度（防止异常情况下的記憶體问题）
const maxLogMessageLength = 10000 // 10KB

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

// SetLevel 設置全局日志级别
func SetLevel(level LogLevel) {
	mu.Lock()
	defer mu.Unlock()
	globalLevel = level

	// 如果設置為DEBUG级别，啟用文件日志
	if level == DEBUG {
		initFileLogger()
	} else {
		closeFileLogger()
	}
}

// SetLocation 設置全局日志時区
func SetLocation(loc *time.Location) {
	locationMu.Lock()
	defer locationMu.Unlock()
	globalLocation = loc
}

// SetLogLanguage 設置日志语言
func SetLogLanguage(lang string) {
	langMu.Lock()
	defer langMu.Unlock()
	if lang != "" {
		logLanguage = lang
	}
}

// GetLogLanguage 獲取日志语言
func GetLogLanguage() string {
	langMu.RLock()
	defer langMu.RUnlock()
	return logLanguage
}

// SetTranslateFunc 設置翻譯函數（由 main 包調用，避免循环依赖）
func SetTranslateFunc(fn func(key string, data ...interface{}) string) {
	translateMu.Lock()
	defer translateMu.Unlock()
	translateFunc = fn
}

// translate 翻譯消息（如果翻譯函數未設置或翻譯失败，返回原始消息）
func translate(message string) string {
	translateMu.RLock()
	fn := translateFunc
	translateMu.RUnlock()

	if fn == nil {
		return message
	}

	// 尝試翻譯，如果失败则返回原始消息
	translated := fn(message)
	if translated == message || translated == "" {
		return message
	}
	return translated
}

// Translate 翻譯消息（導出函數，供其他包使用）
func Translate(key string, data ...interface{}) string {
	translateMu.RLock()
	fn := translateFunc
	translateMu.RUnlock()

	if fn == nil {
		return key
	}

	// 調用翻譯函數
	translated := fn(key, data...)
	if translated == key || translated == "" {
		return key
	}
	return translated
}

// initFileLogger 初始化文件日志（當日志级别為DEBUG時）
func initFileLogger() {
	fileMu.Lock()
	defer fileMu.Unlock()

	// 如果已經初始化且日期相同，不需要重新初始化
	locationMu.RLock()
	loc := globalLocation
	locationMu.RUnlock()

	today := time.Now().In(loc).Format("2006-01-02")
	if fileLogger != nil && currentDate == today {
		return
	}

	// 关闭舊文件
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}

	// 創建log文件夹
	if err := os.MkdirAll(logDir, 0755); err != nil {
		// 如果創建失败，只输出到控制台
		log.Printf("[WARN] 創建日志文件夹失败: %v，將只输出到控制台", err)
		return
	}

	// 創建应用日志文件（按日期命名）
	logFileName := filepath.Join(logDir, fmt.Sprintf("app-quantmesh-%s.log", today))
	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// 如果打开失败，只输出到控制台
		log.Printf("[WARN] 打开日志文件失败: %v，將只输出到控制台", err)
		return
	}

	logFile = file
	currentDate = today
	// 創建文件日志器（不包含時间戳，因為標准log已經包含）
	fileLogger = log.New(file, "", 0)

	log.Printf("[INFO] 文件日志已啟用，日志文件: %s", logFileName)
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

// checkAndRotateLog 检查並輪轉日志文件（如果需要）
// 注意：調用此函數前必須已持有fileMu鎖
func checkAndRotateLog() {
	locationMu.RLock()
	loc := globalLocation
	locationMu.RUnlock()

	today := time.Now().In(loc).Format("2006-01-02")
	if currentDate != today {
		// 日期变化，重新初始化文件日志
		// 关闭舊文件
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}

		// 創建log文件夹
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return
		}

		// 創建新的应用日志文件
		logFileName := filepath.Join(logDir, fmt.Sprintf("app-quantmesh-%s.log", today))
		file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}

		logFile = file
		currentDate = today
		fileLogger = log.New(file, "", 0)
	}
}

// InitLogStorage 初始化日志存儲（通過函數指針避免循环依赖）
func InitLogStorage(writer func(level, message string)) {
	logStorageMu.Lock()
	defer logStorageMu.Unlock()
	logStorageWriter = writer
}

// InitWebLogger 初始化 Web 日志文件
func InitWebLogger() error {
	webFileMu.Lock()
	defer webFileMu.Unlock()

	locationMu.RLock()
	loc := globalLocation
	locationMu.RUnlock()

	today := time.Now().In(loc).Format("2006-01-02")
	
	// 如果已經初始化且日期相同，不需要重新初始化
	if webFileLogger != nil && webCurrentDate == today {
		return nil
	}

	// 关闭舊文件
	if webLogFile != nil {
		webLogFile.Close()
		webLogFile = nil
	}

	// 創建logs文件夹
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("創建日志文件夹失败: %v", err)
	}

	// 創建 Web 日志文件（按日期命名）
	logFileName := filepath.Join(logDir, fmt.Sprintf("web-gin-%s.log", today))
	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("打开 Web 日志文件失败: %v", err)
	}

	webLogFile = file
	webCurrentDate = today
	webFileLogger = log.New(file, "", 0)

	log.Printf("[INFO] Web 日志文件已啟用: %s", logFileName)
	return nil
}

// closeWebLogger 关闭 Web 日志文件
func closeWebLogger() {
	webFileMu.Lock()
	defer webFileMu.Unlock()

	if webLogFile != nil {
		webLogFile.Close()
		webLogFile = nil
		webFileLogger = nil
		webCurrentDate = ""
	}
}

// checkAndRotateWebLog 检查並輪轉 Web 日志文件（如果需要）
// 注意：調用此函數前必須已持有 webFileMu 鎖
func checkAndRotateWebLog() {
	locationMu.RLock()
	loc := globalLocation
	locationMu.RUnlock()

	today := time.Now().In(loc).Format("2006-01-02")
	if webCurrentDate != today {
		// 日期变化，重新初始化 Web 日志文件
		if webLogFile != nil {
			webLogFile.Close()
			webLogFile = nil
		}

		// 創建logs文件夹
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return
		}

		// 創建新的 Web 日志文件
		logFileName := filepath.Join(logDir, fmt.Sprintf("web-gin-%s.log", today))
		file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}

		webLogFile = file
		webCurrentDate = today
		webFileLogger = log.New(file, "", 0)
	}
}

// WriteWebLog 写入 Web 日志（供 Gin 中间件使用）
func WriteWebLog(message string) {
	webFileMu.Lock()
	defer webFileMu.Unlock()

	// 检查是否需要輪轉日志文件
	checkAndRotateWebLog()

	if webFileLogger != nil {
		locationMu.RLock()
		loc := globalLocation
		locationMu.RUnlock()
		
		// 写入文件（包含時间戳）
		webFileLogger.Printf("%s %s", time.Now().In(loc).Format("2006/01/02 15:04:05"), message)
	}
}

// Close 关闭文件日志（程序退出時調用）
func Close() {
	closeFileLogger()
	closeWebLogger()
	// 清理日志存儲写入器
	logStorageMu.Lock()
	defer logStorageMu.Unlock()
	logStorageWriter = nil
}

// GetLevel 獲取全局日志级别
func GetLevel() LogLevel {
	mu.RLock()
	defer mu.RUnlock()
	return globalLevel
}

// shouldLog 判断是否应該输出日志
func shouldLog(level LogLevel) bool {
	return level >= globalLevel
}

// logf 内部日志输出函數
func logf(level LogLevel, format string, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	
	// 使用對象池複用 Builder，减少記憶體分配
	builder := builderPool.Get().(*strings.Builder)
	defer func() {
		builder.Reset()
		builderPool.Put(builder)
	}()
	
	// 構建前缀
	builder.WriteString("[")
	builder.WriteString(level.String())
	builder.WriteString("] ")
	
	// 格式化消息
	formatted := fmt.Sprintf(format, args...)
	builder.WriteString(formatted)
	message := builder.String()
	
	// 限制消息长度，防止异常情况下的記憶體问题
	if len(message) > maxLogMessageLength {
		message = message[:maxLogMessageLength] + "... [truncated]"
	}
	
	// 為了兼容性，也構建 prefix（用於標准输出）
	prefix := fmt.Sprintf("[%s] ", level.String())

	// 输出到控制台（標准输出）
	log.Printf(prefix+format, args...)

	// 如果日志级别為DEBUG，同時写入文件
	if globalLevel == DEBUG {
		fileMu.Lock()
		// 检查是否需要輪轉日志文件
		checkAndRotateLog()
		if fileLogger != nil {
			// 写入文件（包含時间戳）
			locationMu.RLock()
			loc := globalLocation
			locationMu.RUnlock()
			fileLogger.Printf("%s %s", time.Now().In(loc).Format("2006/01/02 15:04:05"), message)
		}
		fileMu.Unlock()
	}

	// 写入 SQLite 數據库（异步，不阻塞）
	logStorageMu.RLock()
	writer := logStorageWriter
	logStorageMu.RUnlock()

	if writer != nil {
		// 使用 goroutine 异步写入，避免阻塞
		go func() {
			defer func() {
				// 恢複 panic，确保不影响主程序
				if r := recover(); r != nil {
					// 静默处理，不输出錯误（避免循环日志）
				}
			}()
			writer(level.String(), message)
		}()
	}
}

// logln 内部日志输出函數（無格式）
func logln(level LogLevel, args ...interface{}) {
	if !shouldLog(level) {
		return
	}
	
	// 使用對象池複用 Builder，减少記憶體分配
	builder := builderPool.Get().(*strings.Builder)
	defer func() {
		builder.Reset()
		builderPool.Put(builder)
	}()
	
	// 構建前缀
	builder.WriteString("[")
	builder.WriteString(level.String())
	builder.WriteString("] ")
	
	// 構建消息
	for i, arg := range args {
		if i > 0 {
			builder.WriteString(" ")
		}
		builder.WriteString(fmt.Sprint(arg))
	}
	message := builder.String()
	
	// 限制消息长度，防止异常情况下的記憶體问题
	if len(message) > maxLogMessageLength {
		message = message[:maxLogMessageLength] + "... [truncated]"
	}
	
	// 為了兼容性，也構建 prefix（用於標准输出）
	prefix := fmt.Sprintf("[%s] ", level.String())

	// 输出到控制台（標准输出）
	log.Println(append([]interface{}{prefix}, args...)...)

	// 如果日志级别為DEBUG，同時写入文件
	if globalLevel == DEBUG {
		fileMu.Lock()
		// 检查是否需要輪轉日志文件
		checkAndRotateLog()
		if fileLogger != nil {
			// 写入文件（包含時间戳，去掉末尾的换行符，因為Println會自动添加）
			locationMu.RLock()
			loc := globalLocation
			locationMu.RUnlock()
			fileLogger.Printf("%s %s", time.Now().In(loc).Format("2006/01/02 15:04:05"), strings.TrimSuffix(message, "\n"))
		}
		fileMu.Unlock()
	}

	// 写入 SQLite 數據库（异步，不阻塞）
	logStorageMu.RLock()
	writer := logStorageWriter
	logStorageMu.RUnlock()

	if writer != nil {
		// 使用 goroutine 异步写入，避免阻塞
		go func() {
			defer func() {
				// 恢複 panic，确保不影响主程序
				if r := recover(); r != nil {
					// 静默处理，不输出錯误（避免循环日志）
				}
			}()
			writer(level.String(), strings.TrimSuffix(message, "\n"))
		}()
	}
}

// Debug 输出調試日志
func Debug(format string, args ...interface{}) {
	logf(DEBUG, format, args...)
}

// Debugln 输出調試日志（無格式）
func Debugln(args ...interface{}) {
	logln(DEBUG, args...)
}

// Info 输出一般信息日志
func Info(format string, args ...interface{}) {
	logf(INFO, format, args...)
}

// Infoln 输出一般信息日志（無格式）
func Infoln(args ...interface{}) {
	logln(INFO, args...)
}

// Warn 输出警告日志
func Warn(format string, args ...interface{}) {
	logf(WARN, format, args...)
}

// Warnln 输出警告日志（無格式）
func Warnln(args ...interface{}) {
	logln(WARN, args...)
}

// Error 输出錯误日志
func Error(format string, args ...interface{}) {
	logf(ERROR, format, args...)
}

// Errorln 输出錯误日志（無格式）
func Errorln(args ...interface{}) {
	logln(ERROR, args...)
}

// Fatal 输出致命錯误日志並退出程序
func Fatal(format string, args ...interface{}) {
	logf(FATAL, format, args...)
	os.Exit(1)
}

// Fatalln 输出致命錯误日志並退出程序（無格式）
func Fatalln(args ...interface{}) {
	logln(FATAL, args...)
	os.Exit(1)
}

// Fatalf 输出致命錯误日志並退出程序（兼容標准库）
func Fatalf(format string, args ...interface{}) {
	Fatal(format, args...)
}
