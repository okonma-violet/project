package logger

import "project/logs/encode"

type PackageLogsContainer struct {
	ch   chan [][]byte
	tags []byte
	list [][]byte
}

func (l *LogsContainer) NewPackageSubLogger(logsBufLen int, tags ...string) PackageLogger {
	return &PackageLogsContainer{ch: l.ch, tags: encode.AppendTags(l.tags, tags...), list: make([][]byte, 0, logsBufLen)}
}

func (l *PackageLogsContainer) Debug(name, logstr string) {
	l.list = append(l.list, encode.EncodeLog(encode.Debug, l.tags, name, logstr))
}

func (l *PackageLogsContainer) Info(name, logstr string) {
	l.list = append(l.list, encode.EncodeLog(encode.Info, l.tags, name, logstr))
}

func (l *PackageLogsContainer) Warning(name, logstr string) {
	l.list = append(l.list, encode.EncodeLog(encode.Warning, l.tags, name, logstr))
}

func (l *PackageLogsContainer) Error(name string, logerr error) {
	var logstr string
	if logerr == nil {
		logstr = logerr.Error()
	} else {
		logstr = "nil err"
	}
	l.list = append(l.list, encode.EncodeLog(encode.Error, l.tags, name, logstr))
}

func (l *PackageLogsContainer) Flush() {
	l.ch <- l.list
}