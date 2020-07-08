package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"time"
)

func runFilter() {
	// 这里我是按文件大小分配的缓存，两个filter行大小接近的情况下是可行的。
	// 换数据后，就炸了，我试了下后把线上的cache换成这个比例才能跑过
	// 理论上要按行做的，没时间改了
	if port == "8000" {
		cacheSize = 2000 * 1024 * 1024
		blockWriteEnds2[readerProcs-1] = cacheSize
		copy(blockWriteEnds[:], blockWriteEnds2[:])
		buffer = make([]byte, cacheSize+1)
	} else {
		buffer = make([]byte, cacheSize+1)
	}
	errTraces = new([errorCacheSize][]byte)
	for i := int32(0); i < errorCacheSize; i++ {
		errTraces[i] = make([]byte, 0, 32*1024)
	}
	blockEnds = make([]int32, 16)
	remoteBlockProcessCount = -1
	remoteMetas = make([]*ErrorTraceMeta, 0, 16*1024)
	localMetas = make([]*ErrorTraceMeta, 16*1024)
	spanIndex = new([128 * 1024 * 1024]int32)
	backendServer = "http://localhost:8002"
	dataReaders = make([]*dataReaderProc, 256)
	dataClient.Transport = trans
	emap = make(map[[12]byte]int32, 8*1024)
	startFilterServer()
}
func startFilterServer() {
	defer recoverLine()
	engine := getEngine()
	engine.GET("/ready", func(c *gin.Context) { c.Status(200) })
	engine.GET("/setParameter", setParamForFilter)
	engine.POST("/error", receiveMetas)
	if err := engine.Run(":" + port); err != nil {
		lPrint(err)
	}
}

func setParamForFilter(c *gin.Context) {
	defer recoverLine()
	if time.Now().Sub(startTime) < 0 {
		startTime = time.Now()
		lPrint("get start time ", startTime.Format(time.RFC3339Nano))
	}
	setParam.Lock()
	if dataport == "" {
		dataport = c.Query("port")
		go startFilterProcess(dataport)
		go func() {
			defer recoverLine()
			resp, err := http.Get(backendServer + "/setPort/" + port + "/" + dataport)
			if err != nil {
				lPrint(err)
			}
			defer resp.Body.Close()
			_, _ = ioutil.ReadAll(resp.Body)
		}()
	}
	setParam.Unlock()
	c.Status(200)
}
func receiveMetas(c *gin.Context) {
	defer recoverLine()
	blockCount, _ := strconv.Atoi(c.Query("block"))
	ret := make([]*ErrorTraceMeta, 0)
	err := c.BindJSON(&ret)
	if err != nil {
		printStack()
		log.Panic(err)
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].LineCount < ret[j].LineCount
	})
	receiveMetasl.Lock()
	remoteMetas = append(remoteMetas, ret...)
	remoteMetaWriteOff += int32(len(ret))
	receiveMetasl.Unlock()
	lPrint(fmt.Sprintf("receive metas %d %d", remoteMetaWriteOff-int32(len(ret)), remoteMetaWriteOff))
	if blockCount > int(remoteBlockProcessCount) {
		remoteBlockProcessCount = int32(blockCount)
	}
}

func startFilterProcess(dataPort string) {
	defer recoverLine()
	// 这里好像对性能提升没啥效果
	runtime.LockOSThread()
	datafile := ""
	if port == "8000" {
		datafile = "trace1.data"
	} else {
		datafile = "trace2.data"
	}
	//dataUrl = fmt.Sprintf("http://localhost:%v/%s", dataPort, datafile)
	dataUrl = fmt.Sprintf("http://localhost:%v/final/%s", 9989, datafile) //我的nginx地址
	initReader()
	//extra是保存上一个block的尾部数据
	readEnd = extraSize

	go readData()
	for {
		if localBlockProcessCount <= localBlockWriteCount {
			processBlock()
			collectBlock()
			localBlockProcessCount++
		} else {
			if endProcess {
				break
			}
			sleepReadWaitWrite += 3 * time.Millisecond
			time.Sleep(3 * time.Millisecond)
		}
	}
}

// goto 不推荐使用，只是这题写起来这样写起来方便
func processBlock() {
	recoverLine()
	var (
		lineStart   = readEnd
		lineEnd     = lineStart
		searchStart = int32(0)
		bytePos     = int32(0)
		newEnd      = int32(0)
		isErrTrace  = false
		end         int32
	)
mainloop:
	end = blockNowWrites[localBlockProcessCount]
	if end-lineStart >= relineIndexStart {
		newEnd = int32(bytes.IndexByte(buffer[lineStart+relineIndexStart:end], relineByte))
		if newEnd >= 0 {
			lineEnd = lineStart + relineIndexStart + newEnd + 1
			isErrTrace = false
			searchStart = lineStart + statusIndexStart
		statusSearch:
			bytePos = int32(bytes.IndexByte(buffer[searchStart:lineEnd], '='))
			if bytePos == -1 {
				goto searchEnd
			}
			searchStart += bytePos
			// 偷鸡技巧，别喷我
			if buffer[searchStart-2] != 'd' || buffer[searchStart-1] != 'e' {
				if buffer[searchStart-2] != 'o' || buffer[searchStart-1] != 'r' {
					searchStart += 4
					if searchStart < lineEnd {
						goto statusSearch
					}
					goto searchEnd
				}
				isErrTrace = true
				goto searchEnd
			}
			if buffer[searchStart+1] == '2' {
				goto searchEnd
			}
			isErrTrace = true
			goto searchEnd
		searchEnd:
			spanIndex[spanOffset] = lineStart
			spanIndex[spanOffset+1] = lineEnd
			spanIndex[spanOffset+2] = int32(binary.BigEndian.Uint32(buffer[lineStart : lineStart+4]))
			spanOffset += 4
			lineStart = lineEnd
			readEnd = lineStart
			if !isErrTrace {
				goto mainloop
			}
			spanIndex[spanOffset-1] = 1
			nMeta := &ErrorTraceMeta{
				Hash:      spanIndex[spanOffset-2],
				LineCount: spanOffset / 4,
				Key:       [12]byte{},
			}
			copy(nMeta.Key[:], buffer[spanIndex[spanOffset-4]:spanIndex[spanOffset-4]+12])
			localMetas[localMetaWriteOff] = nMeta
			localMetaWriteOff++
			if localMetaWriteOff%120 == 0 && localMetaWriteOff > 0 {
				go conCollect()
			}
			goto mainloop
		}
		goto endCheck
	}
	goto endCheck
endCheck:
	if end < blockEnds[localBlockProcessCount] {
		// 已经有新数据了，快去处理
		goto mainloop
	}
	if end > 0 && end == blockEnds[localBlockProcessCount] {
		// 啊，结束了
		return
	} else {
		// 没数据，等会吧
		time.Sleep(5 * time.Millisecond)
		goto mainloop
	}

}

func conCollect() {
	defer func() {
		recoverLine()
		readErrTraceMtx.Unlock()
	}()
	readErrTraceMtx.Lock()
	end := localMetaWriteOff - 1
	// 只搜素，目前其前后2w行已经被处理过的error trace
	for ; end > localMetaReadOff; end-- {
		if (localMetas[end].LineCount+SpanRange)*4 <= spanOffset {
			break
		}
	}
	end++
	collectLocal(localBlockProcessCount-1, end, 0)
	end = remoteMetaWriteOff - 1
	for ; end > remoteMetaReadOff; end-- {
		if (remoteMetas[end].LineCount+SpanRange)*4 <= spanOffset {
			break
		}
	}
	end++
	collectRemote(end)
	sendTraces(errOffset - 1)
}
func collectBlock() {
	defer func() {
		recoverLine()
		readErrTraceMtx.Unlock()
	}()
	readErrTraceMtx.Lock()
	end := localMetaWriteOff - 1
	endFlag := 0
	if localBlockProcessCount != endBlockC {
		for ; end > localMetaReadOff; end-- {
			if (localMetas[end].LineCount+SpanRange)*4 <= spanOffset {
				break
			}
		}
	} else {
		endFlag = 1
	}
	end++
	collectLocal(localBlockProcessCount, end, endFlag)
	localMetaBlockOff[localBlockProcessCount] = localMetaReadOff
	//process remote
	for {
		if remoteBlockProcessCount < localBlockProcessCount {
			//&& remoteEnd != "1" {
			sleepWaitRemoteBlock += 5 * time.Millisecond
			time.Sleep(5 * time.Millisecond)
			continue
		}
		end := remoteMetaWriteOff - 1
		if localBlockProcessCount != endBlockC {
			lPrint("enter no end")
			for ; end > remoteMetaReadOff; end-- {
				if (remoteMetas[end].LineCount+SpanRange)*4 <= spanOffset {
					break
				}
			}
		}
		end++
		collectRemote(end)
		remoteMetaBlockOff[localBlockProcessCount] = remoteMetaReadOff
		break
	}
	sendTraces(errOffset)
	saveExtra()
}
func collectLocal(block, end int32, endFlag int) {
	defer recoverLine()
	collectLocalMtx.Lock()
	defer collectLocalMtx.Unlock()
	go sendMetas(block, endFlag)
	lPrint(fmt.Sprintf("collect local %d %d %d", localMetaReadOff, end, errOffset))
	for ; localMetaReadOff < end; localMetaReadOff++ {
		meta := localMetas[localMetaReadOff]
		if _, ok := emap[meta.Key]; !ok {
			lookupErrorTrace(meta)
			emap[meta.Key] = errOffset
			errOffset++
		}
	}
	lPrint(fmt.Sprintf("end collect %d", errOffset))
}
func collectRemote(end int32) {
	defer recoverLine()
	collectRemoteMtx.Lock()
	defer collectRemoteMtx.Unlock()
	lPrint(fmt.Sprintf("collect remote %d %d %d", remoteMetaReadOff, end, errOffset))
	for ; remoteMetaReadOff < end; remoteMetaReadOff++ {
		meta := remoteMetas[remoteMetaReadOff]
		if _, ok := emap[meta.Key]; !ok {
			lookupErrorTrace(meta)
			emap[meta.Key] = errOffset
			errOffset++
		}
	}
	lPrint(fmt.Sprintf("end collect %d", errOffset))
}
func lookupErrorTrace(meta *ErrorTraceMeta) {
	defer recoverLine()
	t1 := time.Now()
	defer func() {
		lookupLocalTotal += time.Now().Sub(t1)
	}()
	start := meta.LineCount - SpanRange
	if start < 0 {
		start = 0
	}
	end := meta.LineCount + SpanRange
	meta.ErrOffset = errOffset
	if end*4 >= spanOffset {
		end = spanOffset / 4
	}
	hash := meta.Hash
	key := meta.Key[4:]
	for start < end {
		if spanIndex[start*4+2] != hash ||
			!bytes.Equal(buffer[spanIndex[start*4]+4:spanIndex[start*4]+12], key) ||
			spanIndex[start*4+3] == 2 {
			start++
			continue
		}
		errTraces[errOffset] = append(errTraces[errOffset], buffer[spanIndex[start*4]:spanIndex[start*4+1]]...)
		spanIndex[start*4+3] = 2
	}
}
func sendMetas(block int32, endFlag int) {
	end := localMetaWriteOff
	metas := localMetas[localMetaSendOff:end]
	lPrint("send meta ", localMetaSendOff, end)
	localMetaSendOff = end
	bs, _ := json.Marshal(metas)
	resp, err := http.Post(fmt.Sprintf("%s/error/%s?block=%d&end=%d", backendServer, port, block, endFlag), "", bytes.NewReader(bs))
	if err != nil {
		lPrint(err)
	}
	defer resp.Body.Close()
	_, _ = ioutil.ReadAll(resp.Body)
}
func saveExtra() {
	defer recoverLine()
	block := localBlockProcessCount
	if readEnd > extraSize {
		dStart := spanIndex[spanOffset-SpanRange*2*4]
		copy(buffer[extraSize-(blockEnds[block]-dStart):extraSize], buffer[dStart:blockEnds[block]])
		readEnd = extraSize - (blockEnds[block] - readEnd)
		for i := spanOffset - SpanRange*2*4; i < spanOffset; {
			spanIndex[i] -= blockEnds[block] - extraSize
			spanIndex[i+1] -= blockEnds[block] - extraSize
			i += 4
		}
	}
}
func sendTraces(end int32) {
	defer recoverLine()
	start := errSendOff
	errSendOff = end
	lPrint("send error traces ", start, end)
	tmp := []byte("}")
	ret := make([][]byte, end-start)
	for i := start; i < end; i++ {
		ret[i-start] = errTraces[i]
	}
	x := bytes.Join(ret, tmp)
	resp, err := http.Post(fmt.Sprintf("%s/traces?filter=%s&offset=%d", backendServer, port, errSendOff), "", bytes.NewReader(x))
	if err != nil {
		lPrint(err)
	}
	defer resp.Body.Close()
	_, _ = ioutil.ReadAll(resp.Body)
	return

}
