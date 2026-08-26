[English](README.md) | **中文**

# 文件说明
- backend.go backend服务，并支持 panic 后按提交时间恢复的处理机制
- config docker-compose文件，包括跑分程序，注意修改镜像和volume路径 
    - dev-go.yml
    - dev-score.yml
- filter.go 过滤服务
- mockscoring.go mock scoring服务，更快更方便
- reader.go 多线程读取的reader
- retbuffer.go 提交结果的buffer实现，用来加速提交，(但没啥效果:(
- util.go 一些工具类
