package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type dataReaderProc struct {
	rd               io.ReadCloser `json:"-"`
	req              *http.Request `json:"-"`
	reqStart, reqEnd int64
	WriteStart       int32 `json:"WriteStart"`
	WriteEnd         int32 `json:"WriteEnd"`
	NowWrite         int32 `json:"NowWrite"`
	block            int32 `json:"-"`
}

// 按提前规划好的块大小，把文件划分好，包括range范围以及在buffer中写入的范围
func initReader() {
	defer recoverLine()
	dataClient.Transport = trans
	t0 := time.Now()
	resp, err := dataClient.Head(dataUrl)
	if err != nil {
		log.Panic(err)
	}
	bs, _ := json.Marshal(resp.Header)
	logStr += " *& " + string(bs)
	totalLen, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		log.Panic("err")
	}
	nowRead := int64(-1)
	i := int32(0)
	for nowRead < totalLen-1 {
		nowEnd := int64(blockWriteEnds[i%readerProcs])
		var nowStart int64
		if i%readerProcs == 0 {
			nowStart = int64(extraSize)
		} else {
			nowStart = int64(blockWriteEnds[i%readerProcs-1])
		}
		if nowRead+nowEnd-nowStart >= totalLen {
			nowEnd = totalLen - nowRead - 1 + nowStart
		}
		req, err := newDataReq(dataUrl, nowRead+1, nowRead+nowEnd-nowStart)
		if err != nil {
			log.Panic("err")
		}
		dataReaders[i] = &dataReaderProc{
			req:        req,
			WriteStart: int32(nowStart),
			WriteEnd:   int32(nowEnd),
			reqStart:   nowRead + 1,
			reqEnd:     nowRead + nowEnd - nowStart,
			block:      i / readerProcs,
		}
		nowRead = nowRead + nowEnd - nowStart
		i++
	}
	t1 := time.Now()
	lPrint("start conn  time ", t0.Sub(startTime).Milliseconds(),
		"new req time ", t1.Sub(t0).Milliseconds())
	for k, v := range dataReaders {
		if v == nil {
			endBlockC = dataReaders[k-1].block
			lPrint("end block ", endBlockC)
			break
		}
		blockNowWrites[int32(k)/readerProcs] = extraSize
	}
}

// 启动三个协程，循环读取数据
func readData() {
	defer recoverLine()
	wgs := []sync.WaitGroup{{}, {}, {}}
	wgLen := int32(len(wgs))
	allWait := func() {
		for k := range wgs {
			wgs[k].Wait()
		}
	}
	waitAdd := func(i int32) {
		wgs[i%wgLen].Wait()
		wgs[i%wgLen].Add(1)
	}
	t := time.Now()
	defer func() {
		readTotal += time.Now().Sub(t)
	}()
	for i := int32(0); i < int32(len(dataReaders)); i++ {
		if dataReaders[i] == nil {
			allWait()
			endRead = true
			handleEof()
			break
		}
		waitAdd(i)
		if i > readerProcs && i%readerProcs == 2 && dataReaders[i].block > dataReaders[i-3].block {
			localBlockWriteCount++
		}
		go func(procCount int32) {
			defer wgs[procCount%wgLen].Done()
			readerWk(procCount)
		}(i)
	}
}
func initReaderRd(procCount int32) {
	recoverLine()
	proc := dataReaders[procCount]
	resp, err := dataClient.Do(proc.req)
	if err != nil {
		for i := 0; i < 10; i++ {
			resp, err = dataClient.Do(proc.req)
			if err == nil {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
	if err != nil {
		lPrint(err)
		return
	}
	proc.rd = resp.Body
}
func readerWk(procCount int32) {
	defer recoverLine()
	proc := dataReaders[procCount]
	initReaderRd(procCount)
	proc.NowWrite = proc.WriteStart
	writeEnd := proc.WriteEnd
	if proc.block != 0 {
		for localBlockProcessCount < proc.block && getLookedOff(proc.block) <= proc.WriteEnd {
			sleepWriteWaitProcess += 5 * time.Millisecond
			time.Sleep(5 * time.Millisecond)
		}
	}
	for {
		n, err := proc.rd.Read(buffer[proc.NowWrite:writeEnd])
		if n > 0 {
			proc.NowWrite += int32(n)
			setNowWrite(procCount, proc.NowWrite)
		}
		if proc.NowWrite == writeEnd {
			for {
				if setNowWrite(procCount, proc.NowWrite) < proc.NowWrite {
					sleepReaderWait += 1500 * time.Microsecond
					time.Sleep(1500 * time.Microsecond)
				} else {
					if dataReaders[procCount+1] == nil || dataReaders[procCount+1].block > proc.block {
						blockEnds[proc.block] = proc.NowWrite
					}
					break
				}
			}
			return
		}
		if err != nil {
			printStack()
			log.Panic(err)
		}
	}
}

func setNowWrite(procCount, nowWrite int32) int32 {
	if procCount == 0 {
		blockNowWrites[0] = nowWrite
		return nowWrite
	}
	proc := dataReaders[procCount]
	if proc.block > dataReaders[procCount-1].block {
		blockNowWrites[proc.block] = nowWrite
		return nowWrite
	}
	if dataReaders[procCount-1].NowWrite == dataReaders[procCount-1].WriteEnd &&
		blockNowWrites[proc.block] >= dataReaders[procCount-1].WriteEnd &&
		blockNowWrites[proc.block] < nowWrite {
		blockNowWrites[proc.block] = nowWrite
		return nowWrite
	}
	return blockNowWrites[proc.block]
}
func getLookedOff(block int32) int32 {
	if localMetaReadOff < 10 || remoteMetaReadOff < 10 {
		return 0
	}
	if block > 1 &&
		((localMetaBlockOff[block-2] == 0 || localMetaReadOff <= localMetaBlockOff[block-2]+100) ||
			(remoteMetaBlockOff[block-2] == 0 || remoteMetaReadOff <= remoteMetaBlockOff[block-2]+100)) {
		return 0
	}
	x := (localMetas[localMetaReadOff-3].LineCount - SpanRange) * 4
	y := (remoteMetas[remoteMetaReadOff-3].LineCount - SpanRange) * 4
	if x < 0 || y < 0 {
		return 0
	}
	of1 := spanIndex[x]
	of2 := spanIndex[y]
	if of1 < of2 {
		return of1
	}
	return of2
}
func (d *dataReaderProc) MarshalJSON() ([]byte, error) {
	ret := fmt.Sprintf(`{"data":"%d %d %d"}`, d.WriteStart, d.WriteEnd, d.NowWrite)
	return []byte(ret), nil
}
func handleEof() {
	lPrint("end all read time: ", time.Now().Sub(startTime))
	eofWait := 80 * time.Millisecond
	for localBlockProcessCount <= localBlockWriteCount {
		sleepEofWait += eofWait
		time.Sleep(eofWait)
		eofWait /= 2
		if eofWait < 10*time.Millisecond {
			eofWait = 10 * time.Millisecond
		}
	}
	lPrint("end all process time: ", time.Now().Sub(startTime))
	lPrintf("local:%d remote:%d ", localMetaWriteOff, remoteMetaWriteOff)
	endProcess = true
	_, err := http.Get(fmt.Sprintf("http://localhost:%v/status/%s/%s", 8002, port, "end"))
	if err != nil {
		lPrint(err)
	}
	time.Sleep(3 * time.Millisecond)
	printMem()
	b := bytes.ReplaceAll([]byte(logStr), []byte("*&"), []byte{'\n'})
	lPrint(string(b))
	lPrint("sleep time: ", sleepWaitRemoteBlock, sleepReadWaitWrite,
		sleepReaderWait, sleepRemainWait, sleepEofWait, sleepPReadWaitWrite,
		" use time : ", readTotal, processTotal, lookupLocalTotal, lookupRemoteTotal)
	return
}
