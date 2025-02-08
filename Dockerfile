FROM golang:1.23

COPY . .

RUN go mod download

RUN go build -o my-storage-provisioner .

CMD ["/go/my-storage-provisioner"]
