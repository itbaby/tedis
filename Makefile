BIN  := tedis
CMD  := ./cmd/$(BIN)
PKGS := ./...

.PHONY: build test vet lint run clean smoke

build:
	go build -o bin/$(BIN) $(CMD)

vet:
	go vet $(PKGS)

test:
	go test $(PKGS)

lint: vet
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed, skipped"

run: build
	./bin/$(BIN)

# Terminal smoke: run in tmux at 110x30 and dump the pane
smoke: build
	@tmux kill-session -t tedis 2>/dev/null; \
	tmux new-session -d -s tedis -x 110 -y 30 'env -u NO_COLOR ./bin/$(BIN) $(SMOKE_ARGS)'; \
	sleep 2; tmux capture-pane -t tedis -p; tmux kill-session -t tedis

clean:
	rm -rf bin dist
