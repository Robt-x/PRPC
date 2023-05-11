package entity

import (
	"fmt"
	"github.com/bwmarrin/snowflake"
	"time"
)

type Generator struct {
	node *snowflake.Node
}

func (g *Generator) Init(startTime string, machineID int64) (err error) {
	var st time.Time
	st, err = time.Parse("2006-01-02", startTime)
	if err != nil {
		fmt.Println(err)
		return
	}
	snowflake.Epoch = st.UnixNano() / 1e6
	g.node, err = snowflake.NewNode(machineID)
	if err != nil {
		fmt.Println(err)
		return
	}
	return
}

func (g *Generator) GenID() uint64 {
	return uint64(g.node.Generate().Int64())
}
