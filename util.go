package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

func getEngine() *gin.Engine {
	engine := gin.New()
	engine.Use()
	if dev == "1" {
		engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{
			Formatter: func(param gin.LogFormatterParams) string {
				var statusColor, methodColor, resetColor string
				if param.IsOutputColor() {
					statusColor = param.StatusCodeColor()
					methodColor = param.MethodColor()
					resetColor = param.ResetColor()
				}
				if param.Latency > time.Minute {
					// Truncate in a golang < 1.8 safe way
					param.Latency = param.Latency - param.Latency%time.Second
				}
				return fmt.Sprintf("[GIN] %v |%s %3d %s| %13v | %15s |%s %-7s %s %#v\n%s",
					param.TimeStamp.Format(time.RFC3339Nano),
					statusColor, param.StatusCode, resetColor,
					param.Latency,
					param.ClientIP,
					methodColor, param.Method, resetColor,
					param.Path,
					param.ErrorMessage,
				)
			},
		}))
		pprof.Register(engine)
	}
	return engine
}

func recoverLine() {
	if err := recover(); err != nil {
		printStack()
		log.Print(err)
		sendPanicLine()
	}
}

func sendPanicLine() {
	defer func() {
		if err := recover(); err != nil {
			lPrint(err)
		}
	}()
	panicMu.Lock()
	line := getCallerIgnoringLogMulti(2)
	_, _ = http.Get(backendServer + "/panic/" + strconv.Itoa(line))
	time.Sleep(10 * time.Second)
}

func getCallerIgnoringLogMulti(callDepth int) int {
	// the +1 is to ignore this (getCallerIgnoringLogMulti) frame
	return getCaller(callDepth + 1)
}

func getCaller(callDepth int, suffixesToIgnore ...string) int {
	// bump by 1 to ignore the getCaller (this) stackframe
	line := 0
	funcs := ""
	callDepth++
	end := false
outer:
	for {
		var ok bool
		funcs, _, line, ok = caller(callDepth)
		if !ok {
			funcs = "???"
			line = 0
			break
		}
		if end == true {
			if ok, _ := regexp.MatchString("main\\.", funcs); ok {
				break
			}
			callDepth++
			continue outer
		}
		if ok, _ := regexp.MatchString("recoverLine", funcs); !ok {
			callDepth++
			continue outer
		} else {
			callDepth++
			end = true
			continue outer
		}
	}
	return line
}
func caller(skip int) (funcs, file string, line int, ok bool) {
	rpc := make([]uintptr, 1)
	n := runtime.Callers(skip+1, rpc[:])
	if n < 1 {
		return
	}
	frame, _ := runtime.CallersFrames(rpc).Next()
	return frame.Function, frame.File, frame.Line, frame.PC != 0
}
func printStack() {
	var buf [4096]byte
	n := runtime.Stack(buf[:], false)
	log.Printf("==> %s\n", string(buf[:n]))
}

func lPrint(v ...interface{}) {
	if dev == "1" {
		log.Print(v...)
	}
}
func lPrintf(format string, v ...interface{}) {
	if dev == "1" {
		log.Printf(format, v...)
	}
}

func byteSplit(s []byte, sep byte) [][]byte {
	a := make([][]byte, 256)
	i := 0
	defer func() {
		if err := recover(); err != nil {
			print(string(s), string(bytes.Join(a, []byte("{}"))), bytes.IndexByte(s, sep))
		}
	}()
	for {
		m := bytes.IndexByte(s, sep)
		if m < 0 {
			break
		}
		a[i] = s[: m+1 : m+1]
		s = s[m+1:]
		i++
	}
	if len(s) > 0 {
		a[i] = s
		return a[:i+1]
	}
	return a[:i]
}
func printMem() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	bs, err := json.Marshal(ms)
	if err != nil {
		lPrint(err)
	} else {
		lPrint(string(bs))
	}
}
