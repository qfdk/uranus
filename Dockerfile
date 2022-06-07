FROM golang:1.17-alpine
# Set destination for COPY
WORKDIR /app
COPY . .
RUN apk update && apk add git musl-dev gcc make
RUN make
ENV GIN_MODE release
EXPOSE 7777
CMD [ "/app/uranus" ]