package slog

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	grt "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 判断TimeWriter这个结构体是否实现了接口io.WriteCloser
var _ io.WriteCloser = (*LogWriter)(nil)

const (
	compressSuffix = ".gz"
	timeFormat     = "2006-01-02 15:04:05"
)

// LogWriter CompressReserveDay是不包含ReserveDay的
type LogWriter struct {
	dir                string
	prefix             string
	compress           bool
	reserveDay         int
	compressReserveDay int

	curFilename string
	file        *os.File
	mu          sync.Mutex
	startMill   sync.Once
	millCh      chan bool
}

type OptFunc func(logWriter *LogWriter)

func Init() {
	writers := []io.Writer{
		NewLogWriters(),
		os.Stdout,
	}
	fileAndStdoutWriter := io.MultiWriter(writers...)
	log.SetOutput(fileAndStdoutWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func NewLogWriters() *LogWriter {
	var dir string
	if grt.GOOS == "linux" {
		dir = "./log"
	}
	if grt.GOOS == "windows" || grt.GOOS == "darwin" {
		dir = "./log"
	}
	LogWriter, _ := NewLogWriter(
		Dir(dir),
		Prefix("camkeep"),
		CompressReserveDay(30),
		ReserveDay(7),
		Compress(true),
	)
	writers := []io.Writer{
		LogWriter,
		os.Stdout,
	}
	fileAndStdoutWriter := io.MultiWriter(writers...)
	log.SetOutput(fileAndStdoutWriter)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return LogWriter
}

func Dir(value string) OptFunc {
	return func(logWriter *LogWriter) {
		logWriter.dir = value
	}
}

func Prefix(value string) OptFunc {
	return func(logWriter *LogWriter) {
		logWriter.prefix = value
	}
}

func Compress(value bool) OptFunc {
	return func(logWriter *LogWriter) {
		logWriter.compress = value
	}
}

func ReserveDay(value int) OptFunc {
	return func(logWriter *LogWriter) {
		logWriter.reserveDay = value
	}
}

func CompressReserveDay(value int) OptFunc {
	return func(logWriter *LogWriter) {
		logWriter.compressReserveDay = value
	}
}

func NewLogWriter(options ...OptFunc) (*LogWriter, error) {
	logWriter := &LogWriter{}
	for _, optFunc := range options {
		optFunc(logWriter)
	}
	return logWriter, nil
}

// 实现io.WriterCloser需要实现两个方法，Writer和Close
func (l *LogWriter) Write(p []byte) (n int, err error) {
	// 锁，日志是多线程写入的
	l.mu.Lock()
	defer l.mu.Unlock()

	// 启动，或者不明原因file没了，重新打开原有file，或者时间到了，新建
	if l.file == nil {
		if err = l.openExistingOrNew(); err != nil {
			fmt.Printf("写日志异常, 原因：\n%s", err)
			return 0, err
		}
	}
	// 获取一下此刻应该写入的日志文件的名称，如果与当前持有的不同，则更新一下
	if l.curFilename != l.filename() {
		// rotate频率很低，所以这里不传入文件名，直接再生成一次也是可以的
		// 新生成日志文件时才会去删除旧日志文件，删除过期日志文件
		_ = l.rotate()
	}
	// 日志信息写入文件，n表示写入字符串的字节数
	n, err = l.file.Write(p)

	return n, err
}

// Close 实现io.WriterCloser需要实现两个方法，Writer和Close
func (l *LogWriter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.close()
}

// close的具体方法
func (l *LogWriter) close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// 打开原有文件，或者新的日志文件（不存在或者打开失败）
func (l *LogWriter) openExistingOrNew() error {
	filename := l.filename()
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return l.openNew()
	} else if err != nil {
		return fmt.Errorf("打开现有日志文件或新建日志文件失败，原因： %s", err)
	}
	// os.O_APPEND 新增模式
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// 旧有文件存在，但是打开有问题，直接新建一个
		return l.openNew()
	}
	l.curFilename = filename
	l.file = file
	return nil
}

// 新建日志文件
func (l *LogWriter) openNew() error {
	name := l.filename()
	err := os.MkdirAll(l.dir, 0744)
	if err != nil {
		return fmt.Errorf("创建日志目录失败，原因: %s", err)
	}

	mode := os.FileMode(0644)
	// os.O_CREATE 不存在会自动创建
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("打开日志文件出错，原因: %s", err)
	}
	l.curFilename = name
	l.file = f
	return nil
}

func (l *LogWriter) rotate() error {
	// 先关闭原有文件
	if err := l.close(); err != nil {
		return err
	}
	// 再打开新的文件
	if err := l.openNew(); err != nil {
		return err
	}
	l.mill()
	return nil
}

func (l *LogWriter) filename() string {
	year, month, day := time.Now().Date()
	date := fmt.Sprintf("%04d%02d%02d", year, month, day)
	// os.Args[0]获取的是可执行文件的名字，所以最好还是不要用main
	if l.prefix == "" {
		l.prefix = filepath.Base(os.Args[0])
	}
	name := fmt.Sprintf("%s.%s.log", l.prefix, date)
	// 如果指定了日志文件目录，就放到指定目录；否则，放到临时目录，而不是/var/log
	// win和linux都有自己的临时目录，但是类似/var/log这些目录都是不可控的
	if l.dir != "" {
		return filepath.Join(l.dir, name)
	}
	return filepath.Join(os.TempDir(), name)
}

func (l *LogWriter) mill() {
	// sync.one确保millCh只初始化一次
	l.startMill.Do(func() {
		l.millCh = make(chan bool, 1)
		go l.millRun()
	})
	// 这边放入一个true，下面millRun就可以执行一次
	select {
	case l.millCh <- true:
	default:
	}
}

func (l *LogWriter) millRun() {
	// 对通道的range，会一直等待读取，而不是当前读完了就没了
	// 并且，通道本身就是指针传递
	for range l.millCh {
		_ = l.millRunOnce()
	}
}

// 压缩、删除文件
func (l *LogWriter) millRunOnce() error {
	// 日志保留ReserveDay后压缩成gz文件保存，直到保存到CompressReserveDay之后删除
	// 获取日志文件列表，剔除当前日志文件，包含已压缩的文件
	files, err := l.oldLogFiles()
	if err != nil {
		return err
	}
	var compress, remove []logInfo
	var outDateNum int
	// 如果要压缩，就用 压缩文件保存时间+正常日志文件保存时间 作为过期删除时间
	if l.compress {
		outDateNum = l.compressReserveDay + l.reserveDay
	} else {
		outDateNum = l.reserveDay
	}
	diff := time.Hour * time.Duration(outDateNum*24)
	outDate := time.Now().Add(-1 * diff)
	// 转换到当日零点
	outDate = time.Date(outDate.Year(), outDate.Month(), outDate.Day(), 0, 0, 0, 0, outDate.Location())
	var remaining []logInfo
	for _, f := range files {
		if f.timestamp.Before(outDate) {
			// 需要删除的文件
			remove = append(remove, f)
		} else {
			remaining = append(remaining, f)
		}
	}
	files = remaining
	// 提取要压缩的文件，前面已经去掉了待删除文件
	// 如果CompressReserveDay为0，ReserveDay也必然为0，即每天都立即压缩
	if l.compress {
		diff := time.Hour * time.Duration(l.reserveDay*24)
		reserve := time.Now().Add(-1 * diff)
		reserve = time.Date(reserve.Year(), reserve.Month(), reserve.Day(), 0, 0, 0, 0, reserve.Location())
		for _, f := range files {
			if f.timestamp.Before(reserve) && !strings.HasSuffix(f.Name(), compressSuffix) {
				compress = append(compress, f)
			}
		}
	}
	// 删除日志文件
	for _, f := range remove {
		errRemove := os.Remove(filepath.Join(l.dir, f.Name()))
		if err == nil && errRemove != nil {
			err = errRemove
		}
	}
	// 压缩未压缩的日志文件
	for _, f := range compress {
		fn := filepath.Join(l.dir, f.Name())
		errCompress := compressLogFile(fn, fn+compressSuffix)
		if err == nil && errCompress != nil {
			err = errCompress
		}
	}

	return err
}

// 读取旧日志文件
func (l *LogWriter) oldLogFiles() ([]logInfo, error) {
	files, err := ioutil.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录下的文件列表失败: %s", err)
	}
	var logFiles []logInfo

	// 获取文件名前缀和文件格式（例如/var/log/user.20200810.log获取的就是user和log）
	// 用来过滤当前文件夹下的其他文件，以及已经压缩的文件
	prefix, ext := l.prefixAndExt()

	for _, f := range files {
		// 目录，跳过
		if f.IsDir() {
			continue
		}
		// 当前文件，跳过
		if f.Name() == filepath.Base(l.curFilename) {
			continue
		}
		if t, err := l.timeFromName(f.Name(), prefix, ext); err == nil {
			logFiles = append(logFiles, logInfo{t, f})
			continue
		}
		if t, err := l.timeFromName(f.Name(), prefix, ext+compressSuffix); err == nil {
			logFiles = append(logFiles, logInfo{t, f})
			continue
		}
	}

	// byFormatTime就是[]logInfo，但是实现了排序方法，按时间降序排列
	sort.Sort(byFormatTime(logFiles))

	return logFiles, nil
}

// 获取文件名前缀和文件格式（例如/var/log/user.20200810.log获取的就是user和log）
func (l *LogWriter) prefixAndExt() (prefix, ext string) {
	// 文件名
	filename := filepath.Base(l.filename())
	// 扩展名（文件格式）
	ext = filepath.Ext(filename)
	prefix = filename[:len(filename)-len(ext)-8]
	return prefix, ext
}

func (l *LogWriter) timeFromName(filename, prefix, ext string) (time.Time, error) {
	// 根据“前缀.日期.后缀”的格式剔除不是本工程日志的文件
	if !strings.HasPrefix(filename, prefix) {
		return time.Time{}, errors.New("根据前缀名称判定文件不属于本工程")
	}
	if !strings.HasSuffix(filename, ext) {
		return time.Time{}, errors.New("根据格式判定不是日志文件")
	}
	ts := filename[len(prefix) : len(filename)-len(ext)]
	if len(ts) != 8 {
		return time.Time{}, errors.New("从文件名获取不到正确的日期")
	}
	if year, err := strconv.ParseInt(ts[0:4], 10, 64); err != nil {
		return time.Time{}, err
	} else if month, err := strconv.ParseInt(ts[4:6], 10, 64); err != nil {
		return time.Time{}, err
	} else if day, err := strconv.ParseInt(ts[6:8], 10, 64); err != nil {
		return time.Time{}, err
	} else {
		timeStr := fmt.Sprintf("%04d-%02d-%02d 00:00:00", year, month, day)
		if location, err := time.LoadLocation("Local"); err != nil {
			return time.Time{}, err
		} else if t, err := time.ParseInLocation(timeFormat, timeStr, location); err != nil {
			return time.Time{}, err
		} else {
			return t, nil
		}
	}

}

func compressLogFile(src, dst string) (err error) {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %v", err)
	}
	defer f.Close()

	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("获取日志文件信息失败: %v", err)
	}

	gzf, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
	if err != nil {
		return fmt.Errorf("打开待压缩文件失败: %v", err)
	}
	defer gzf.Close()

	gz := gzip.NewWriter(gzf)

	defer func() {
		if err != nil {
			_ = os.Remove(dst)
			err = fmt.Errorf("压缩文件失败: %v", err)
		}
	}()

	if _, err := io.Copy(gz, f); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := gzf.Close(); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}

	return nil
}

type logInfo struct {
	timestamp time.Time
	os.FileInfo
}

// byFormatTime sorts by newest time formatted in the name.
type byFormatTime []logInfo

func (b byFormatTime) Less(i, j int) bool {
	return b[i].timestamp.After(b[j].timestamp)
}

func (b byFormatTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byFormatTime) Len() int {
	return len(b)
}
