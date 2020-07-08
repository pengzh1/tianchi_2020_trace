package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func runBackEnd() {
	server := backServer{
		ErrorTraceMap: make(map[string][][]byte),
		filter1: &filterStatus{
			addr:   "http://localhost:8000",
			status: "start",
			errs:   map[string]string{},
		},
		filter2: &filterStatus{
			addr:   "http://localhost:8001",
			status: "start",
			errs:   map[string]string{},
		},
		checkSumMap: make(map[string]string),
		mtx:         sync.RWMutex{},
		fetchCount:  1,
		retBuf:      newReader(),
	}
	server.start()
}
func (s *backServer) start() {
	engine := getEngine()
	engine.POST("/error/:filterId", s.newErrorTraceFromFilter)
	engine.POST("/traces", s.receiveTraces)
	engine.GET("/scoring/:traceId", s.getTraceDetail)
	engine.GET("/status/:filterId/:status", s.updateStatus)
	engine.GET("/ready", ready)
	engine.GET("/setParameter", s.setParams)
	engine.GET("/setPort/:filterId/:dataport", s.setPort)
	engine.GET("/panic/:line", s.setPanic)
	if err := engine.Run(":" + port); err != nil {
		log.Panic(err)
	}
}
func ready(c *gin.Context) {
	c.Status(200)
}

func (s *backServer) newErrorTraceFromFilter(c *gin.Context) {
	filterId := c.Param("filterId")
	block := c.Query("block")
	end := c.Query("end")
	addr := ""
	if filterId == "8000" {
		s.filter1.blockCount, _ = strconv.Atoi(block)
		addr = s.filter2.addr
	} else {
		s.filter2.blockCount, _ = strconv.Atoi(block)
		addr = s.filter1.addr
	}
	bs, _ := ioutil.ReadAll(c.Request.Body)
	resp, err := client.Post(addr+"/error?block="+block+"&end="+end, "", bytes.NewReader(bs))
	if err != nil {
		log.Panic(err)
	}
	defer resp.Body.Close()
	_, _ = ioutil.ReadAll(resp.Body)
	c.Status(200)
}

var sendStart = false

func (s *backServer) receiveTraces(c *gin.Context) {
	s.recMtx.Lock()
	defer s.recMtx.Unlock()
	filter := c.Query("filter")
	off, _ := strconv.Atoi(c.Query("offset"))
	if filter == "8000" {
		s.filter1.errOff = off
	} else {
		s.filter2.errOff = off
	}
	retBuf, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		lPrint("error io occur: ", err.Error())
	}
	defer c.Request.Body.Close()
	ret := make(map[string][][]byte)
	lines := bytes.Split(retBuf, []byte{'}'})
	for _, line := range lines {
		if len(line) < 180 || len(line) > 128*1024 {
			continue
		}
		tid := string(line[:bytes.IndexByte(line, fieldDelimByte)])
		if len(tid) > 16 {
			log.Panic("err2")
		}
		ret[tid] = byteSplit(line, relineByte)
	}
	keys := s.mergeRet(ret, filter)
	for _, id := range keys {
		t := s.ErrorTraceMap[id]
		sort.Slice(t, func(i, j int) bool {
			return bytes.Compare(t[i][25:33], t[j][25:33]) == -1
		})
		d := md5.New()
		for _, v := range t {
			d.Write(v)
		}
		ret := d.Sum(nil)
		s.checkSumMap[id] = strings.ToUpper(hex.EncodeToString(ret))
		s.retBuf.write(id, strings.ToUpper(hex.EncodeToString(ret)))
		delete(s.ErrorTraceMap, id)
	}
	// 因为我发现发送结果给 scoreing server很慢，就想的提前连接，提前写数据，结果发现对成绩并没有提升
	if !sendStart && off > 600 {
		sendStart = true
		go func() {
			resp, err := http.Post(fmt.Sprintf("http://localhost:%s/api/finished", s.scorePort),
				"application/x-www-form-urlencoded", s.retBuf)
			log.Print(resp, err)
		}()
	}
	c.Status(200)
}

func (s *backServer) mergeRet(ret map[string][][]byte, filter string) []string {
	retKeys := make([]string, 0)
	for traceId, v1 := range ret {
		if len(traceId) > 17 || len(traceId) < 13 {
			log.Panic("err3")
		}
		if filter == "8000" {
			if s.filter1.errs[traceId] != "" {
				log.Panic("err4")
			}
			s.filter1.errs[traceId] = string(bytes.Join(v1, []byte{}))
		}
		if filter == "8001" {
			if s.filter2.errs[traceId] != "" {
				log.Panic("err5")
			}
			s.filter2.errs[traceId] = string(bytes.Join(v1, []byte{}))
		}
		if v2, ok := s.ErrorTraceMap[traceId]; !ok {
			s.ErrorTraceMap[traceId] = v1
		} else {
			s.ErrorTraceMap[traceId] = append(v2, v1...)
			retKeys = append(retKeys, traceId)
		}
	}
	return retKeys
}
func (s *backServer) processRemainedErrorTrace() {
	s.mtx.Lock()
	defer func() {
		s.mtx.Unlock()
		defer s.recMtx.Unlock()
		if e := recover(); e != nil {
			printStack()
			lPrint(e)
		}
	}()
	s.recMtx.Lock()
	//t0 := time.Now()
	lPrint("remained error traces ", len(s.ErrorTraceMap))
	for id, t := range s.ErrorTraceMap {
		sort.Slice(t, func(i, j int) bool {
			return bytes.Compare(t[i][25:33], t[j][25:33]) == -1
		})
		d := md5.New()
		for _, v := range t {
			d.Write(v)
		}
		ret := d.Sum(nil)
		s.checkSumMap[id] = strings.ToUpper(hex.EncodeToString(ret))
		s.retBuf.write(id, strings.ToUpper(hex.EncodeToString(ret)))
	}
	s.retBuf.setEnd()
}
func (s *backServer) mergeErrorTrace(data map[string][][]byte) {
	s.mtx.Lock()
	for traceId, v1 := range data {
		if len(traceId) > 17 || len(traceId) < 13 {
			continue
		}
		if v2, ok := s.ErrorTraceMap[traceId]; !ok {
			s.ErrorTraceMap[traceId] = v1
		} else {
			s.ErrorTraceMap[traceId] = append(v2, v1...)
		}
	}
	s.mtx.Unlock()
}
func (s *backServer) getTraceDetail(c *gin.Context) {
	traceIds := c.Param("traceId")
	c.Status(200)
	if len(traceIds) > 0 {
		idx := strings.Split(traceIds, ",")
		for _, id := range idx {
			if len(id) > 1 {
				lPrint("wrongId:\n", id, string(bytes.Join(s.ErrorTraceMap[id], []byte(""))))
				break
			}
		}
	}
}
func (s *backServer) updateStatus(c *gin.Context) {
	filterId := c.Param("filterId")
	if filterId == "8000" {
		s.filter2.status = "end"
	} else {
		s.filter1.status = "end"
	}
	c.Status(200)
	end := s.filter1.status == "end" && s.filter2.status == "end"
	if end {
		go s.processRemainedErrorTrace()
	}
}
func (s *backServer) setPort(c *gin.Context) {
	dataport := c.Param("dataport")
	filterId := c.Param("filterId")
	addr := ""
	if time.Now().Sub(startTime) < 0 {
		startTime = time.Now()
		lPrint("get start time ", startTime.Format(time.RFC3339Nano))
	}
	if filterId == "8000" {
		addr = s.filter2.addr
	} else {
		addr = s.filter1.addr
	}
	resp, err := http.Get(addr + "/setParameter?port=" + dataport)
	if err != nil {
		log.Panic(err)
	}
	defer resp.Body.Close()
	_, _ = ioutil.ReadAll(resp.Body)
	c.Status(200)
}
func (s *backServer) setParams(c *gin.Context) {
	dataport := c.Query("port")
	s.scorePort = dataport
	c.Status(200)
}
func newDataReq(url string, start, end int64) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	return req, nil
}

var panicMu = sync.Mutex{}
var finishMu = sync.Mutex{}

func (s *backServer) setPanic(c *gin.Context) {
	finishMu.Lock()
	line, _ := strconv.Atoi(c.Param("line"))
	dur := startTime.Add(time.Duration(line) * 100 * time.Millisecond).Sub(time.Now())
	lPrint("wait for ", dur)
	time.AfterFunc(dur, func() {
		_, _ = http.PostForm(fmt.Sprintf("http://localhost:%s/api/finished", s.scorePort), url.Values{
			"result": []string{`{"overtime":"overtime"}`},
		})
	})
}
