FROM public.ecr.aws/docker/library/golang:1.23.1-bullseye as builder

WORKDIR /workspace
COPY . .
RUN GOPROXY=direct go mod download

RUN CGO_ENABLED=0 go build

FROM public.ecr.aws/docker/library/alpine:3.20.3

COPY --from=builder /workspace/aws-pod-eip-controller /usr/local/bin/aws-pod-eip-controller
CMD ["aws-pod-eip-controller"]
