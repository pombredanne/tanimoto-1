package tanimoto


import (
	"context"
	"errors"

	"container/heap"
	"github.com/pilosa/pilosa"
	"math"
	"github.com/pilosa/pilosa/pql"
)

// Tanimoto represents a plugin that will find the common bits of the top-n list.
type TanimotoPlugin struct {
	executor *pilosa.Executor
}

// NewTanimotoPlugin returns a new instance of DiffTopPlugin.
func NewTanimotoPlugin(e *pilosa.Executor) pilosa.Plugin {
	return &TanimotoPlugin{e}
}

// Map executes the plugin against a single slice.
func (p *TanimotoPlugin) Map(ctx context.Context, index string, call *pql.Call, slice uint64) (interface{}, error) {

	var frame string
	var threshold uint64
	args := call.Args
	if fr, found := args["frame"]; found {
		frame = fr.(string)
	} else {
		return nil, errors.New("frame required")
	}

	if thres, found := args["threshold"]; found {
		if thres.(int64) > 100 {
			return nil, errors.New("threshold is from 1 to 100")
		}
		threshold = uint64(thres.(int64))
	} else {
		return nil, errors.New("threshold required")
	}

	bm, err := p.executor.ExecuteCallSlice(ctx, index, call.Children[0], slice, p)
	if err != nil {
		return nil, err
	}

	frag := p.executor.Holder.Fragment(index, frame, pilosa.ViewStandard, slice)
	opt := pilosa.TopOptions{TanimotoThreshold: threshold, Src: bm.(*pilosa.Bitmap)}

	pairs := frag.Cache().Top()
	var tanimotoThreshold uint64
	var minTanimoto, maxTanimoto float64
	var srcCount uint64
	if opt.TanimotoThreshold > 0 && opt.Src != nil {
		tanimotoThreshold = opt.TanimotoThreshold
		srcCount = opt.Src.Count()
		minTanimoto = float64(srcCount*tanimotoThreshold) / 100
		maxTanimoto = float64(srcCount*100) / float64(tanimotoThreshold)
	}
	var rr []pilosa.Pair
	results := &pilosa.PairHeap{}
	for _, pair := range pairs {
		rowID, cnt := pair.ID, pair.Count
		if tanimotoThreshold > 0 {
			if float64(cnt) <= minTanimoto || float64(cnt) >= maxTanimoto {
				continue
			}
			count := opt.Src.IntersectionCount(frag.Row(rowID))
			if count == 0 {
				continue
			}
			tanimoto := math.Ceil(float64(count*100) / float64(cnt+srcCount-count))
			if tanimoto <= float64(tanimotoThreshold) {
				continue
			}
			rr = append(rr, pilosa.Pair{ID: rowID, Count: cnt})
			heap.Push(results, pilosa.Pair{ID: rowID, Count: cnt})
		}
	}

	r := make(pilosa.Pairs, results.Len(), results.Len())
	x := results.Len()
	i := 1
	for results.Len() > 0 {
		r[x-i] = heap.Pop(results).(pilosa.Pair)
		i++
	}

	return rr, nil
}

// Reduce combines previous map results into a single value.
func (p *TanimotoPlugin) Reduce(ctx context.Context, prev, v interface{}) interface{} {

	switch x := v.(type) {
	case *pilosa.Bitmap:
		if prev != nil {
			bm := prev.(*pilosa.Bitmap)
			return bm.Union(x)
		}
		return x
	case int:
		return x
	}
	return v
}

