.PHONY: build run clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o chowkidar-agent .

run:
	./chowkidar-agent

clean:
	rm -f chowkidar-agent
