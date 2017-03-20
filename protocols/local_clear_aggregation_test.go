package protocols_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/JoaoAndreSa/MedCo/lib"
	"github.com/JoaoAndreSa/MedCo/protocols"
	"github.com/stretchr/testify/assert"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/network"
	"strconv"
	"github.com/dedis/cothority/log"
)


func TestLocalClearAggregation(t *testing.T) {
	local := onet.NewLocalTest()
	_, _, tree := local.GenTree(1, true)

	defer local.CloseAll()

	rootInstance, err := local.CreateProtocol("LocalClearAggregation", tree)
	if err != nil {
		t.Fatal("Couldn't start protocol:", err)
	}
	protocol := rootInstance.(*protocols.LocalClearAggregationProtocol)

	testData := generateClearData()
	aggregatedData := lib.AddInClear(testData)

	protocol.TargetOfAggregation = testData
	feedback := protocol.FeedbackChannel

	go protocol.Start()

	timeout := network.WaitRetry * time.Duration(network.MaxRetryConnect*5*2) * time.Millisecond

	select {
	case results := <-feedback:
	log.LLvl1(results)
	log.LLvl1(aggregatedData)
		assert.Equal(t, compareClearResponses(results, aggregatedData), true)
	case <-time.After(timeout):
		t.Fatal("Didn't finish in time")
	}
}

func generateClearData() []lib.DpClearResponse {
	testData := make([]lib.DpClearResponse, 6)



	testData[0] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 1}), GroupByClear: simpleMapFromTab("g",[]int64{1, 1}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{1, 2, 3, 4, 5})}
	log.LLvl1(testData[0])
	testData[1] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 2}), GroupByClear: simpleMapFromTab("g",[]int64{1, 2}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{0, 1, 4, 3, 0})}
	testData[2] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 3}), GroupByClear: simpleMapFromTab("g",[]int64{1, 3}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{0, 1, 0, 1, 0})}

	testData[3] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 1}), GroupByClear: simpleMapFromTab("g",[]int64{1, 1}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{0, 0, 0, 0, 0})}
	testData[4] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 2}), GroupByClear: simpleMapFromTab("g",[]int64{1, 2}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{1, 3, 5, 7, 1})}
	testData[5] = lib.DpClearResponse{WhereClear: simpleMapFromTab("w",[]int64{1, 3}), GroupByClear: simpleMapFromTab("g",[]int64{1, 3}), AggregatingAttributesClear: simpleMapFromTab("s",[]int64{1, 0, 1, 0, 1})}

	return testData
}

func simpleMapFromTab(name string, tab []int64) map[string]int64{
	map1 := make(map[string]int64)
	for i,v := range tab{
		map1[name+strconv.Itoa(i)] = v
	}
	return map1
}

func compareClearResponses(x []lib.DpClearResponse, y []lib.DpClearResponse) bool {
	var test bool
	for _, i := range x {
		test = false
		for _, j := range y {
			if reflect.DeepEqual(i, j) {
				test = true
				break
			}
		}

		if !test {
			break
		}
	}

	return test
}
