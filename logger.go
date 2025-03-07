package logvin

import (
	"flag"
	"fmt"
	"github.com/natefinch/lumberjack"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/writer"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultConfigFile   = "./config/logger.yaml"
	RootLogger          = "root"
	Stdout              = "Stdout"
	Console             = "Console"
	TextFormatter       = "TextFormatter"
	JSONFormatter       = "JSONFormatter"
	CorvinFormatterName = "CorvinFormatter"
)

// configFile It can be set by the flag -logger-config
var configFile = defaultConfigFile

// formatter logrus.Formatter Type
var formatter = map[string]FormatterFunc{
	TextFormatter: FormatterFunc(func(*LoggerConfig) logrus.Formatter {
		return new(logrus.TextFormatter)
	}),
	JSONFormatter: FormatterFunc(func(*LoggerConfig) logrus.Formatter {
		return new(logrus.JSONFormatter)
	}),
	CorvinFormatterName: FormatterFunc(func(config *LoggerConfig) logrus.Formatter {
		return &CorvinFormatter{
			Config: config,
		}
	}),
}

func init() {
	args, found := os.Args[1:], false
	for len(args) > 0 {
		name := args[0]
		args = args[1:]
		if name == "-logger-config" {
			if len(args) > 0 {
				configFile = args[0]
			}
			found = true
			break
		}
		if after, ok := strings.CutPrefix(name, "-logger-config="); ok {
			configFile = after
			found = true
			break
		}
	}
	if found && !flag.Parsed() {
		//默认CommandLine的errorHandling是ExitOnError,在默认CommandLine里再定义一次logger-config，防止解析参数时报错
		flag.CommandLine.StringVar(new(string), "logger-config", defaultConfigFile, "define logger config file")
	}
}

var instance *Config
var once sync.Once

var appenders = make(map[string]*Appender)
var loggers = make(map[string]*Logger)
var mux sync.Mutex

type Logger struct {
	*logrus.Logger
	Config *LoggerConfig
}

type Appender struct {
	io.Writer
	Name string
}

type FormatterFunc func(*LoggerConfig) logrus.Formatter

func RegisterFormatter(name string, fun FormatterFunc) bool {
	formatter[name] = fun
	log.Printf("register formatter: %s", name)
	return true
}

func (c Config) String() string {
	data, err := yaml.Marshal(c)
	if err != nil {
		return ""
	}
	return string(data)
}

type Config struct {
	Appenders []AppenderConfig `yaml:"appenders"`
	Loggers   LoggersConfig    `yaml:"loggers"`
}

type AppenderConfig struct {
	Name       string `yaml:"name"`
	Output     string `yaml:"output"`
	MaxSize    int    `yaml:"maxSize"`
	MaxAge     int    `yaml:"maxAge"`
	MaxBackups int    `yaml:"maxBackups"`
	LocalTime  bool   `yaml:"localtime"`
	Compress   bool   `yaml:"compress"`
}

type LoggersConfig struct {
	Root   LoggerConfig   `yaml:"root"`
	Logger []LoggerConfig `yaml:"logger"`
}

type LoggerConfig struct {
	Name      string `yaml:"name"`
	Level     string `yaml:"level"`
	Caller    bool   `yaml:"caller"`
	Console   bool   `yaml:"console"`
	Formatter string `yaml:"formatter"`
	Appender  string `yaml:"appender"`
}

type HookAppenderConfig struct {
	Name  string `yaml:"name"`
	Level string `yaml:"level"`
}

func newDefaultConfig() *Config {
	return &Config{
		Appenders: []AppenderConfig{
			{
				Name:   Stdout,
				Output: Stdout,
			},
		},
		Loggers: LoggersConfig{
			Root: LoggerConfig{
				Name:      RootLogger,
				Level:     logrus.InfoLevel.String(),
				Caller:    false,
				Console:   true,
				Formatter: TextFormatter,
				Appender:  Stdout,
			},
			Logger: []LoggerConfig{},
		},
	}
}

func getConfig() *Config {
	once.Do(func() {
		data, err := os.ReadFile(configFile)
		if err != nil {
			log.Printf("read file: %s error: %v", configFile, err)
			instance = newDefaultConfig()
			return
		}
		instance = new(Config)
		if err := yaml.Unmarshal(data, instance); err != nil {
			log.Printf("Unmarshal Config error: %v", err)
			instance = newDefaultConfig()
		} else {
			log.Printf("load %s success. %s", configFile, instance.String())
		}
		initAppender(instance.Appenders)
	})
	return instance
}

func initAppender(appenderConfig []AppenderConfig) {
	for _, config := range appenderConfig {
		if appender, err := newAppender(config); err == nil {
			appenders[config.Name] = appender
		} else {
			log.Printf(err.Error())
		}
	}
}

func newAppender(config AppenderConfig) (*Appender, error) {
	if strings.ToLower(config.Output) == strings.ToLower(Console) ||
		strings.ToLower(config.Output) == strings.ToLower(Stdout) {
		return &Appender{
			Writer: os.Stdout,
			Name:   config.Name,
		}, nil
	}
	if strings.HasPrefix(config.Output, "file:/") {
		return newFileAppender(config)
	}
	if strings.HasPrefix(config.Output, "rotate:/") {
		return newRotateAppender(config)
	}
	return nil, fmt.Errorf("un-defined: Appender: %s", config.Name)
}

func newFileAppender(config AppenderConfig) (*Appender, error) {
	fileName, ok := strings.CutPrefix(config.Output, "file:/")
	if !ok {
		return nil, fmt.Errorf("illegal file output. %s", config.Output)
	}
	dir := filepath.Dir(fileName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		//创建目录
		if err = os.MkdirAll(dir, os.ModePerm); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(fileName); err == nil {
		log.Printf("FileAppender[%s] file-name:%s, absolute-path:%s", config.Name, fileName, abs)
	}
	return &Appender{
		Writer: file,
		Name:   config.Name,
	}, nil
}

func newRotateAppender(config AppenderConfig) (*Appender, error) {
	fileName, ok := strings.CutPrefix(config.Output, "rotate:/")
	if !ok {
		return nil, fmt.Errorf("illegal file output. %s", config.Output)
	}
	logger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    config.MaxSize, // 兆字节
		MaxAge:     config.MaxAge,  // 天数
		MaxBackups: config.MaxBackups,
		LocalTime:  config.LocalTime,
		Compress:   config.Compress,
	}
	if abs, err := filepath.Abs(fileName); err == nil {
		log.Printf("RotateAppender[%s] absolute-path:%s", config.Name, abs)
	}
	return &Appender{
		Writer: logger,
		Name:   config.Name,
	}, nil
}

func New(name string) *Logger {
	mux.Lock()
	defer mux.Unlock()
	if logger, ok := loggers[name]; ok {
		return logger
	}
	var loggerConfig *LoggerConfig
	for _, lc := range getConfig().Loggers.Logger {
		if lc.Name == name {
			loggerConfig = &lc
			break
		}
	}
	if loggerConfig == nil {
		loggerConfig = &getConfig().Loggers.Root
	}
	rus := logrus.New()
	//日志级别
	if len(loggerConfig.Level) > 0 {
		if level, err := logrus.ParseLevel(loggerConfig.Level); err == nil {
			rus.SetLevel(level)
		}
	}
	//调用者信息
	rus.SetReportCaller(loggerConfig.Caller)
	//输出格式
	if len(loggerConfig.Formatter) > 0 {
		if fun, ok := formatter[loggerConfig.Formatter]; ok {
			rus.SetFormatter(fun(loggerConfig))
		}
	}
	//appender
	if appender, ok := appenders[loggerConfig.Appender]; ok {
		rus.SetOutput(appender)
	} else {
		log.Printf("%s logger un-found appender %s", loggerConfig.Name, loggerConfig.Appender)
		rus.SetOutput(os.Stdout)
	}
	logger := &Logger{rus, loggerConfig}
	loggers[name] = logger
	log.Printf("New Logger: %s", name)
	return logger
}

// newHook support mul-append
func newHook(logger LoggerConfig, loggerAppender HookAppenderConfig) (logrus.Hook, error) {
	lvl := loggerAppender.Level
	if len(lvl) == 0 {
		lvl = logger.Level
	}
	levels := make([]logrus.Level, 0)
	if level, err := logrus.ParseLevel(lvl); err == nil {
		for _, ll := range logrus.AllLevels {
			if ll <= level {
				levels = append(levels, ll)
			}
		}
	}
	if appender, ok := appenders[loggerAppender.Name]; ok {
		return &writer.Hook{
			Writer:    appender,
			LogLevels: levels,
		}, nil
	} else {
		return nil, fmt.Errorf("%s logger un-found appender", loggerAppender.Name)
	}
}

type CorvinFormatter struct {
	Config *LoggerConfig
}

func (f *CorvinFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	timeLayout := "2006-01-02 15:04:05.000"
	if f.Config.Caller && entry.Caller != nil {
		format := "%s[%s][%s:%d] %s\r\n"
		return []byte(fmt.Sprintf(format, entry.Time.Format(timeLayout), strings.ToUpper(entry.Level.String()),
			entry.Caller.Function, entry.Caller.Line, entry.Message)), nil
	}
	format := "%s[%s] %s\r\n"
	return []byte(fmt.Sprintf(format, entry.Time.Format(timeLayout), strings.ToUpper(entry.Level.String()), entry.Message)), nil
}
