package main

import (
	"fmt"
	"os"
	"time"

	"github.com/masa23/gotail"
)

// init construct
const (
	LogFile        = "./test.log"
	PosFile        = "./test.log.pos"
	TailErrLogFile = "./tail_error.log"
)

func main() {
	go func() {
		fd, err := os.OpenFile(LogFile, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}
		defer fd.Close()
		fd.Truncate(0)
		fd.Seek(0, 0)

		for {
			t := time.Now().String()
			fd.WriteString(t + "\n")
			fd.Sync()
			time.Sleep(time.Second)
		}
	}()
	tail, err := gotail.Open(LogFile, PosFile)
	if err != nil {
		panic(err)
	}

	err = tail.SetLog(TailErrLogFile)
	if err != nil {
		panic(err)
	}

	tail.Scan()

	for {
		b := tail.TailString()
		fmt.Println(b)
	}

}
