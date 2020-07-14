# 文件说明
- backend.go backend服务，还支持了panic to commit time的骚操作
- config docker-compose文件，包括跑分程序，注意修改镜像和volume路径 
    - dev-go.yml
    - dev-score.yml
- filter.go 过滤服务
- mockscoring.go mock scoring服务，更快更方便
- reader.go 多线程读取的reader
- retbuffer.go 提交结果的buffer实现，用来加速提交，(但没啥效果:(
- util.go 一些工具类
