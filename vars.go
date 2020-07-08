package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type ErrorTraceMeta struct {
	Hash      int32    `json:"hash"`
	LineCount int32    `json:"lineCount"`
	Key       [12]byte `json:"key"`
	ErrOffset int32    `json:"-"`
}
type backServer struct {
	ErrorTraceMap map[string][][]byte
	filter1       *filterStatus
	filter2       *filterStatus
	checkSumMap   map[string]string
	scorePort     string
	mtx           sync.RWMutex
	recMtx        sync.RWMutex
	fetchCount    int
	checkSum      map[string]string
	retBuf        *retReader
}
type filterStatus struct {
	addr       string
	errs       map[string]string
	status     string
	blockCount int
	errOff     int
}

const (
	errorCacheSize   = int32(4096)
	statusIndexStart = int32(100)
	relineIndexStart = int32(125)
	relineByte       = '\n'
	fieldDelimByte   = '|'
	extraSize        = int32(32 * 1024 * 1024)
	// 这里设1w行也不会有错trace
	SpanRange = int32(20000)
)

// search patterns
var (
	backendServer string
	receiveMetasl = sync.Mutex{}
	emap          map[[12]byte]int32
)

//error cache
var (
	errTraces  *[errorCacheSize][]byte
	errOffset  int32
	errSendOff int32
)

//trie and block
var (
	spanIndex  *[128 * 1024 * 1024]int32
	spanOffset int32
	buffer     []byte
	blockEnds  []int32
	readEnd    int32
	setParam   = sync.Mutex{}
	dataport   = ""
)

//debug
var (
	dev        = "1"
	endProcess = false
	logStr     string
	startTime  = time.Now().Add(1 * time.Hour)
)
var (
	dataUrl                 string
	dataClient              http.Client
	localBlockWriteCount    int32
	localBlockProcessCount  int32
	remoteBlockProcessCount int32
	remoteMetas             []*ErrorTraceMeta
	remoteMetaWriteOff      int32
	remoteMetaReadOff       int32
	localMetas              []*ErrorTraceMeta
	localMetaWriteOff       int32
	localMetaSendOff        int32
	localMetaReadOff        int32
	localMetaBlockOff       = make([]int32, 16, 16)
	remoteMetaBlockOff      = make([]int32, 16, 16)
	readErrTraceMtx         sync.RWMutex
	readTotal               time.Duration
	processTotal            time.Duration
	lookupLocalTotal        time.Duration
	lookupRemoteTotal       time.Duration
	nowWriteStart           int32
	sleepWriteWaitProcess   time.Duration
	sleepWaitRemoteBlock    time.Duration
	sleepReadWaitWrite      time.Duration
	sleepReaderWait         time.Duration
	sleepRemainWait         time.Duration
	sleepEofWait            time.Duration
	sleepPReadWaitWrite     time.Duration
	dataReaders             = make([]*dataReaderProc, 0)
	trans                   = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          6,
		MaxConnsPerHost:       16,
		IdleConnTimeout:       10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ReadBufferSize:        32 * 1024,
	}
	collectLocalMtx  sync.RWMutex
	collectRemoteMtx sync.RWMutex
	cacheSize        = int32(1500 * 1024 * 1024)
	blockWriteEnds   = []int32{
		42 * 1024 * 1024,
		70 * 1024 * 1024,
		120 * 1024 * 1024,
		200 * 1024 * 1024,
		360 * 1024 * 1024,
		650 * 1024 * 1024,
		1000 * 1024 * 1024,
		cacheSize,
	}
	readerProcs     = int32(8)
	blockWriteEnds2 = []int32{
		42 * 1024 * 1024,
		70 * 1024 * 1024,
		120 * 1024 * 1024,
		200 * 1024 * 1024,
		360 * 1024 * 1024,
		650 * 1024 * 1024,
		1200 * 1024 * 1024,
		cacheSize,
	}
	blockNowWrites       = make([]int32, 8)
	endBlockC      int32 = 0
	endRead              = false
	call                 = false
	client               = http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:          4,
			MaxConnsPerHost:       6,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
)
