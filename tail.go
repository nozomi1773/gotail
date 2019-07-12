package gotail

import (
	"bufio"
	"io/ioutil"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"

	"github.com/masa23/gotail/error"
)

// Tail is tail file struct
type Tail struct {
	file                   string
	fileFd                 *os.File
	posFile                string
	posFd                  *os.File
	Stat                   Stat
	data                   chan []byte
	isCreatePosFile        bool
	InitialReadPositionEnd bool // If true, there is no pos file Start reading from the end of the file
	logFile                string
	logFd                  *os.File
}

// Stat tail stats infomation struct
type Stat struct {
	Inode  uint64 `yaml:"Inode"`
	Offset int64  `yaml:"Offset"`
	Size   int64  `yaml:"Size"`
}

// Open file and position files.
func Open(file string, posfile string) (*Tail, error) {
	var err error
	t := Tail{file: file, posFile: posfile}

	// open position file
	t.posFd, err = os.OpenFile(t.posFile, os.O_RDWR, 0644)
	if err != nil && !os.IsNotExist(err) {
		return &t, errors.Wrap(err, gotail.ErrPosFileOpenFailed)
	} else if os.IsNotExist(err) {
		t.posFd, err = os.OpenFile(t.posFile, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return &t, errors.Wrap(err, gotail.ErrPosFileOpenFailed)
		}
		t.isCreatePosFile = true
	}
	posdata, err := ioutil.ReadAll(t.posFd)
	if err != nil {
		return &t, errors.Wrap(err, gotail.ErrPosFileOpenFailed)
	}
	posStat := Stat{}
	yaml.Unmarshal(posdata, &posStat)

	// open tail file.
	t.fileFd, err = os.Open(t.file)
	if err != nil {
		return &t, errors.Wrap(err, gotail.ErrTailFileOpenFailed)
	}

	// get file stat
	fdStat, err := t.fileFd.Stat()
	if err != nil {
		return &t, errors.Wrap(err, gotail.ErrGetFileStatFailed)
	}
	stat := fdStat.Sys().(*syscall.Stat_t)

	// file stat
	t.Stat.Inode = stat.Ino
	t.Stat.Size = stat.Size
	if stat.Ino == posStat.Inode && stat.Size >= posStat.Size {
		// If the inode is not changed, restart from the subsequent Offset.
		t.Stat.Offset = posStat.Offset
	} else {
		// If the file size is small, set the offset to 0.
		t.Stat.Offset = 0
	}

	// update position file
	err = posUpdate(&t)
	if err != nil {
		return &t, errors.Wrap(err, gotail.ErrPosFileUpdateFailed)
	}

	// tail seek posititon.
	t.fileFd.Seek(t.Stat.Offset, os.SEEK_SET)

	return &t, nil
}

// Close is file and position file close.
func (t *Tail) Close() error {
	err := t.posFd.Close()
	if err != nil {
		return errors.Wrap(err, gotail.ErrPosFileCloseFailed)
	}
	err = t.fileFd.Close()
	if err != nil {
		return errors.Wrap(err, gotail.ErrTailFileCloseFailed)
	}
	if t.logFd != nil {
		err = t.logFd.Close()
		if err != nil {
			return errors.Wrap(err, gotail.ErrLogFileCloseFailed)
		}
	}

	return nil
}

// PositionUpdate is pos file update
func (t *Tail) PositionUpdate() error {
	err := posUpdate(t)
	return err
}

func posUpdate(t *Tail) error {
	t.posFd.Truncate(0)
	t.posFd.Seek(0, 0)

	yml, err := yaml.Marshal(&t.Stat)
	if err != nil {
		return errors.Wrap(err, gotail.ErrPosFileUpdateFailed)
	}

	t.posFd.Write(yml)
	if err != nil {
		return errors.Wrap(err, gotail.ErrPosFileUpdateFailed)
	}

	t.posFd.Sync()
	return nil
}

// TailBytes is get one line bytes.
func (t *Tail) TailBytes() []byte {
	return <-t.data
}

// TailString is get one line strings.
func (t *Tail) TailString() string {
	return string(<-t.data)
}

// Set log file for logging library unexpected error while scanning
func (t *Tail) SetLog(logfile string) error {
	fd, err := os.OpenFile(logfile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return errors.Wrap(err, gotail.ErrLogFileOpenFailed)
	}
	t.logFile = logfile
	t.logFd = fd
	return nil
}

// Scan is start scan.
func (t *Tail) Scan() {
	if t.logFd != nil {
		log.SetOutput(t.logFd)
	} else {
		log.SetOutput(os.Stdout)
	}
	log.SetFlags(log.Ldate | log.Ltime)

	// there is no pos file Start reading from the end of the file
	if t.InitialReadPositionEnd && t.isCreatePosFile {
		t.fileFd.Seek(0, os.SEEK_END)
	}
	t.data = make(chan []byte)
	go func() {
		var err error
		for {
			scanner := bufio.NewScanner(t.fileFd)
			for scanner.Scan() {
				scbytes := scanner.Bytes()
				data := make([]byte, len(scbytes))
				copy(data, scanner.Bytes())
				t.data <- data
			}
			if err := scanner.Err(); err != nil {
				err := recover()
				if t.logFd != nil {
					log.Printf("%v : %v\n", gotail.ErrScanFailed, err)
				}
				continue
			}

			t.Stat.Offset, err = t.fileFd.Seek(0, os.SEEK_CUR)
			if err != nil {
				if t.logFd != nil {
					log.Printf("%v : %v\n", gotail.ErrScanFailed, err)
				}
				continue
			}

			fd, err := os.Open(t.file)
			if os.IsNotExist(err) {
				time.Sleep(time.Millisecond * 10)
				continue
			} else if err != nil {
				err := recover()
				if t.logFd != nil {
					log.Printf("%v : %v\n", gotail.ErrScanFailed, err)
				}
			}
			fdStat, err := fd.Stat()
			if err != nil {
				err := recover()
				if t.logFd != nil {
					log.Printf("%v : %v\n", gotail.ErrScanFailed, err)
				}
			}
			stat := fdStat.Sys().(*syscall.Stat_t)
			if stat.Ino != t.Stat.Inode {
				t.Stat.Inode = stat.Ino
				t.Stat.Offset = 0
				t.Stat.Size = stat.Size
				t.fileFd.Close()
				t.fileFd = fd
			} else {
				if stat.Size < t.Stat.Size {
					t.fileFd.Seek(0, os.SEEK_SET)
				}
				t.Stat.Size = stat.Size
				time.Sleep(time.Millisecond * 10)
				fd.Close()
			}

			err = posUpdate(t)
			if err != nil {
				err := recover()
				if t.logFd != nil {
					log.Printf("%v : %v\n", gotail.ErrScanFailed, err)
				}
			}
		}
	}()
}
