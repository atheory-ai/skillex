.PHONY: setup test test-perf clean

setup:
	./setup.sh

test: setup
	cd .. && go build -o skillex ./cmd/skillex
	cd .. && go test ./test/acceptance/... -v -timeout 300s

test-perf:
	./setup.sh --perf
	cd .. && go build -o skillex ./cmd/skillex
	cd .. && go test ./test/acceptance/ -run "TestPerformance" -v -timeout 600s

clean:
	./setup.sh --clean
