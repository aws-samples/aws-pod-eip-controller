FROM public.ecr.aws/docker/library/golang:1.24.4-bullseye AS builder

WORKDIR /workspace
COPY . .
RUN GOPROXY=direct go mod download

RUN CGO_ENABLED=0 go build

FROM public.ecr.aws/docker/library/alpine:3.22.0

ENV USER=eipcontroller
ENV GROUPNAME=$USER
ENV UID=5000
ENV GID=5000

COPY --from=builder /workspace/aws-pod-eip-controller /usr/local/bin/aws-pod-eip-controller

RUN addgroup \
    --gid "$GID" \
    "$GROUPNAME" \
&&  adduser \
    --disabled-password \
    --gecos "" \
    --home "$(pwd)" \
    --ingroup "$GROUPNAME" \
    --no-create-home \
    --uid "$UID" \
    $USER

USER $UID

CMD ["aws-pod-eip-controller"]
