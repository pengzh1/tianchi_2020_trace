package main

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type scoringServer struct {
	sum      map[string]string
	servers  map[string]int
	mtx      sync.RWMutex
	datapath string
	start    time.Time
}

func runScoring() {
	datapath := os.Getenv("DATA_PATH")
	if datapath == "" {
		datapath = "/tmp"
	}
	bs, err := ioutil.ReadFile(datapath + "/checkSum.json")
	if err != nil {
		log.Panic(err)
	}
	ret := make(map[string]string)
	err = json.Unmarshal(bs, &ret)
	if err != nil {
		log.Panic(err)
	}
	s := scoringServer{
		sum:      ret,
		mtx:      sync.RWMutex{},
		datapath: datapath,
		servers: map[string]int{
			"http://localhost:8000": 0,
			"http://localhost:8001": 0,
			"http://localhost:8002": 0,
		},
	}
	engine := gin.Default()
	engine.POST("/api/finished", s.finished)
	engine.GET("/:filename", s.FileDownload)
	go func() {
		s.checkStart()
	}()
	if err := engine.Run(":" + port); err != nil {
		log.Panic(err)
	}
}
func (s *scoringServer) FileDownload(c *gin.Context) {
	filename := c.Param("filename")
	c.Writer.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Writer.Header().Add("Content-Type", "application/octet-stream")
	if filename == "trace1.data" {
		c.File(s.datapath + "/trace1.data")
	}
	if filename == "trace2.data" {
		c.File(s.datapath + "/trace2.data")
	}
}
func (s *scoringServer) checkStart() {
	for u := range s.servers {
		for i := 0; i < 10; i++ {
			resp, err := http.Get(u + "/ready")
			if err != nil {
				log.Print(err)
				time.Sleep(1 * time.Second)
			} else if resp.StatusCode != http.StatusOK {
				log.Print(resp.Status)
				time.Sleep(1 * time.Second)
			} else {
				s.servers[u] = 1
				break
			}
		}
		if s.servers[u] != 1 {
			log.Panic(fmt.Sprintf("failed start because:%v", u))
		}
	}
	s.start = time.Now()
	for u := range s.servers {
		resp, err := http.Get(u + "/setParameter?port=" + port)
		if err != nil {
			log.Panic(err)
		} else if resp.StatusCode != http.StatusOK {
			log.Panic(err)
		}
	}
}
func (s *scoringServer) finished(c *gin.Context) {
	if ret, ok := c.GetPostForm("result"); !ok {
		c.Status(400)
		return
	} else {
		end := time.Now()
		log.Print(time.Now().Format(time.RFC3339Nano))
		ioutil.WriteFile(s.datapath+"/newCheckSum.json", []byte(ret), 0644)
		rec := make(map[string]string)
		err := json.Unmarshal([]byte(ret), &rec)
		if err != nil {
			c.Status(500)
			return
		}
		s.getScore(rec, end)
		c.String(200, "suc")
	}

}
func (s *scoringServer) getScore(ret map[string]string, end time.Time) {
	var (
		right    int64 = 0
		wrong    int64 = 0
		miss     int64 = 0
		no       int64 = 0
		f1       int64 = 0
		all            = int64(len(s.sum))
		wrongIds []string
	)
	for k, v := range ret {
		if rv, ok := s.sum[k]; !ok {
			log.Printf("nofor:%s,", k)
			wrongIds = append(wrongIds, k)
			no++
		} else if rv != v {
			log.Printf("wrongfor:%s,get:%s,expected:%s", k, v, rv)
			wrongIds = append(wrongIds, k)
			delete(s.sum, k)
			wrong++
		} else {
			delete(s.sum, k)
			right++
		}
	}
	miss = all - right - wrong
	f1 = 4 * 100000 * all
	if wrong > 0 || no > 0 || miss > 0 {
		f1 = 0
	}
	dur := end.Sub(s.start).Milliseconds()
	score := f1 / dur
	for k := range s.sum {
		log.Print("miss ", k)
	}
	log.Printf("Score:%v,F1:%v,Duration:%v ms, right:%v, wrong:%v, miss:%v, no:%v \n",
		score, f1, dur, right, wrong, miss, no)
	go func() {
		ids := strings.Join(wrongIds, ",")
		resp, err := http.Get("http://localhost:8002/scoring/" + ids)
		if err != nil {
			log.Panic(err)
		}
		defer resp.Body.Close()
		_, _ = ioutil.ReadAll(resp.Body)
	}()
	return
}
