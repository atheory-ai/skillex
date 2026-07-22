.PHONY: setup test test-perf clean

setup:
	./setup.sh

test: setup
	cd .. && $(MAKE) dev-binary
	cd .. && SKILLEX_BINARY="$$(pwd)/.skillex/bin/skillex" go test ./test/acceptance/... -v -timeout 300s

test-perf:
	./setup.sh --perf
	cd .. && $(MAKE) dev-binary
	cd .. && SKILLEX_BINARY="$$(pwd)/.skillex/bin/skillex" go test ./test/acceptance/ -run "TestPerformance" -v -timeout 600s

clean:
	./setup.sh --clean
