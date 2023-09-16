FROM public.ecr.aws/docker/library/golang:1.19.6 as builder

COPY go.mod go.sum /workspace/
WORKDIR /workspace
ENV GOPROXY="https://goproxy.cn"
RUN go mod download

COPY main.go main.go
COPY pkg pkg

RUN GO111MODULE=on CGO_ENABLED=0 go build -a -o aws-pod-eip-controller

FROM public.ecr.aws/amazonlinux/amazonlinux:2023
WORKDIR /root/
COPY --from=builder /workspace/aws-pod-eip-controller /root/
COPY config.yaml /etc/aws-pod-eip-controller/config.yaml

RUN chmod +x /root/aws-pod-eip-controller
CMD /root/aws-pod-eip-controller -f /etc/aws-pod-eip-controller/config.yaml