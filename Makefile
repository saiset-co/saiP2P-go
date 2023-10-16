## make binaries and dirs for local test
local-test-build:
## build first local instance
	@rm -rf dist
	@mkdir dist
	GOOS=${OSFLAG} GOARCH=${OSHW} go build -o dist/grape *.go
	@cp config.yml dist/

## build second local instance
	@rm -rf dist2
	@mkdir dist2
	@cp config.yml dist2/
	@ mv dist2/config.yml dist2/config.yml
	@cp dist/grape dist2/grape

## build third local instance
	@rm -rf dist3
	@mkdir dist3
	@cp dist/grape dist3/grape
	@cp config.yml dist3/
	@ mv dist3/config.yml dist3/config.yml
