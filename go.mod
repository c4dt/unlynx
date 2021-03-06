module github.com/ldsec/unlynx

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/Knetic/govaluate v3.0.0+incompatible
	github.com/fanliao/go-concurrentMap v0.0.0-20141114143905-7d2d7a5ea67b
	github.com/r0fls/gostats v0.0.0-20180711082619-e793b1fda35c
	github.com/satori/go.uuid v1.2.0
	github.com/stretchr/testify v1.3.0
	github.com/urfave/cli v1.22.1
	go.dedis.ch/kyber/v3 v3.0.5
	go.dedis.ch/onet/v3 v3.0.24
)

//replace go.dedis.ch/onet/v3 => ../../../go.dedis.ch/onet
