package sysexit

import (
	"fmt"
	"os"

	"github.com/sean-/sysexits"
)

type SysExit struct {
	Code  int
	Error error
}

func Handle() {
	if e := recover(); e != nil {
		if exit, ok := e.(SysExit); ok == true {
			fmt.Fprintf(os.Stderr, "%s\nsysexits(3) error code %d\n", exit.Error.Error(), exit.Code)
			os.Exit(int(exit.Code))
		}
		panic(e) // not an Exit, pass-through
	}
}

func CreateNew(err error, code int) SysExit {
	return SysExit{Code: code, Error: err}
}

func Unavailable(err error) SysExit {
	return SysExit{Code: sysexits.Unavailable, Error: err}
}

func Os(err error) SysExit {
	return SysExit{Code: sysexits.OSErr, Error: err}
}

func File(err error) SysExit {
	return SysExit{Code: sysexits.OSFile, Error: err}
}

func Create(err error) SysExit {
	return SysExit{Code: sysexits.CantCreate, Error: err}
}

func Config(err error) SysExit {
	return SysExit{Code: sysexits.Config, Error: err}
}

func Protocol(err error) SysExit {
	return SysExit{Code: sysexits.Protocol, Error: err}
}

func NoPerm(err error) SysExit {
	return SysExit{Code: sysexits.NoPerm, Error: err}
}

func TempFail(err error) SysExit {
	return SysExit{Code: sysexits.TempFail, Error: err}
}
