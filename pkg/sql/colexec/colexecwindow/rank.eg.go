// Code generated by execgen; DO NOT EDIT.
// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the CockroachDB Software License
// included in the /LICENSE file.

package colexecwindow

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/col/coldata"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/colexecbase"
	"github.com/cockroachdb/cockroach/pkg/sql/colexec/colexecutils"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecerror"
	"github.com/cockroachdb/cockroach/pkg/sql/colexecop"
	"github.com/cockroachdb/cockroach/pkg/sql/colmem"
	"github.com/cockroachdb/cockroach/pkg/sql/execinfrapb"
	"github.com/cockroachdb/cockroach/pkg/sql/sem/tree"
	"github.com/cockroachdb/cockroach/pkg/sql/types"
	"github.com/cockroachdb/errors"
)

// Remove unused warning.
var _ = colexecerror.InternalError

// TODO(yuzefovich): add benchmarks.

// NewRankOperator creates a new Operator that computes window functions RANK
// or DENSE_RANK (depending on the passed in windowFn).
// outputColIdx specifies in which coldata.Vec the operator should put its
// output (if there is no such column, a new column is appended).
func NewRankOperator(
	args *WindowArgs,
	windowFn execinfrapb.WindowerSpec_WindowFunc,
	orderingCols []execinfrapb.Ordering_Column,
) (colexecop.Operator, error) {
	if len(orderingCols) == 0 {
		return colexecbase.NewConstOp(
			args.MainAllocator, args.Input, types.Int, int64(1), args.OutputColIdx)
	}
	input := colexecutils.NewVectorTypeEnforcer(
		args.MainAllocator, args.Input, types.Int, args.OutputColIdx)
	initFields := rankInitFields{
		OneInputNode:    colexecop.NewOneInputNode(input),
		allocator:       args.MainAllocator,
		outputColIdx:    args.OutputColIdx,
		partitionColIdx: args.PartitionColIdx,
		peersColIdx:     args.PeersColIdx,
	}
	switch windowFn {
	case execinfrapb.WindowerSpec_RANK:
		if args.PartitionColIdx != tree.NoColumnIdx {
			return &rankWithPartitionOp{rankInitFields: initFields}, nil
		}
		return &rankNoPartitionOp{rankInitFields: initFields}, nil
	case execinfrapb.WindowerSpec_DENSE_RANK:
		if args.PartitionColIdx != tree.NoColumnIdx {
			return &denseRankWithPartitionOp{rankInitFields: initFields}, nil
		}
		return &denseRankNoPartitionOp{rankInitFields: initFields}, nil
	default:
		return nil, errors.AssertionFailedf("unsupported rank type %s", windowFn)
	}
}

type rankInitFields struct {
	colexecop.OneInputNode
	colexecop.InitHelper

	allocator       *colmem.Allocator
	outputColIdx    int
	partitionColIdx int
	peersColIdx     int
}

type rankNoPartitionOp struct {
	rankInitFields

	// rank indicates which rank should be assigned to the next tuple.
	rank int64
	// rankIncrement indicates by how much rank should be incremented when a
	// tuple distinct from the previous one on the ordering columns is seen.
	rankIncrement int64
}

var _ colexecop.Operator = &rankNoPartitionOp{}

func (r *rankNoPartitionOp) Init(ctx context.Context) {
	if !r.InitHelper.Init(ctx) {
		return
	}
	r.Input.Init(r.Ctx)
	// All rank functions start counting from 1. Before we assign the rank to a
	// tuple in the batch, we first increment r.rank, so setting this
	// rankIncrement to 1 will update r.rank to 1 on the very first tuple (as
	// desired).
	r.rankIncrement = 1
}

func (r *rankNoPartitionOp) Next() coldata.Batch {
	batch := r.Input.Next()
	n := batch.Length()
	if n == 0 {
		return coldata.ZeroBatch
	}
	peersCol := batch.ColVec(r.peersColIdx).Bool()
	rankVec := batch.ColVec(r.outputColIdx)
	rankCol := rankVec.Int64()
	sel := batch.Selection()
	if sel != nil {
		for _, i := range sel[:n] {
			if peersCol[i] {
				r.rank += r.rankIncrement
				r.rankIncrement = 1
				rankCol[i] = r.rank
			} else {
				rankCol[i] = r.rank
				r.rankIncrement++
			}
		}
	} else {
		_ = peersCol[n-1]
		_ = rankCol[n-1]
		for i := 0; i < n; i++ {
			//gcassert:bce
			if peersCol[i] {
				r.rank += r.rankIncrement
				r.rankIncrement = 1
				//gcassert:bce
				rankCol[i] = r.rank
			} else {
				//gcassert:bce
				rankCol[i] = r.rank
				r.rankIncrement++
			}
		}
	}
	return batch
}

type rankWithPartitionOp struct {
	rankInitFields

	// rank indicates which rank should be assigned to the next tuple.
	rank int64
	// rankIncrement indicates by how much rank should be incremented when a
	// tuple distinct from the previous one on the ordering columns is seen.
	rankIncrement int64
}

var _ colexecop.Operator = &rankWithPartitionOp{}

func (r *rankWithPartitionOp) Init(ctx context.Context) {
	if !r.InitHelper.Init(ctx) {
		return
	}
	r.Input.Init(r.Ctx)
	// All rank functions start counting from 1. Before we assign the rank to a
	// tuple in the batch, we first increment r.rank, so setting this
	// rankIncrement to 1 will update r.rank to 1 on the very first tuple (as
	// desired).
	r.rankIncrement = 1
}

func (r *rankWithPartitionOp) Next() coldata.Batch {
	batch := r.Input.Next()
	n := batch.Length()
	if n == 0 {
		return coldata.ZeroBatch
	}
	partitionCol := batch.ColVec(r.partitionColIdx).Bool()
	peersCol := batch.ColVec(r.peersColIdx).Bool()
	rankVec := batch.ColVec(r.outputColIdx)
	rankCol := rankVec.Int64()
	sel := batch.Selection()
	if sel != nil {
		for _, i := range sel[:n] {
			if partitionCol[i] {
				// We need to reset the internal state because of the new partition.
				// Note that the beginning of new partition necessarily starts a new
				// peer group, so peersCol[i] *must* be true, and we will correctly
				// update the rank before setting it to rankCol.
				r.rank = 0
				r.rankIncrement = 1
			}
			if peersCol[i] {
				r.rank += r.rankIncrement
				r.rankIncrement = 1
				rankCol[i] = r.rank
			} else {
				rankCol[i] = r.rank
				r.rankIncrement++
			}
		}
	} else {
		_ = partitionCol[n-1]
		_ = peersCol[n-1]
		_ = rankCol[n-1]
		for i := 0; i < n; i++ {
			//gcassert:bce
			if partitionCol[i] {
				// We need to reset the internal state because of the new partition.
				// Note that the beginning of new partition necessarily starts a new
				// peer group, so peersCol[i] *must* be true, and we will correctly
				// update the rank before setting it to rankCol.
				r.rank = 0
				r.rankIncrement = 1
			}
			//gcassert:bce
			if peersCol[i] {
				r.rank += r.rankIncrement
				r.rankIncrement = 1
				//gcassert:bce
				rankCol[i] = r.rank
			} else {
				//gcassert:bce
				rankCol[i] = r.rank
				r.rankIncrement++
			}
		}
	}
	return batch
}

type denseRankNoPartitionOp struct {
	rankInitFields

	// rank indicates which rank should be assigned to the next tuple.
	rank int64
	// rankIncrement indicates by how much rank should be incremented when a
	// tuple distinct from the previous one on the ordering columns is seen.
	rankIncrement int64
}

var _ colexecop.Operator = &denseRankNoPartitionOp{}

func (r *denseRankNoPartitionOp) Init(ctx context.Context) {
	if !r.InitHelper.Init(ctx) {
		return
	}
	r.Input.Init(r.Ctx)
	// All rank functions start counting from 1. Before we assign the rank to a
	// tuple in the batch, we first increment r.rank, so setting this
	// rankIncrement to 1 will update r.rank to 1 on the very first tuple (as
	// desired).
	r.rankIncrement = 1
}

func (r *denseRankNoPartitionOp) Next() coldata.Batch {
	batch := r.Input.Next()
	n := batch.Length()
	if n == 0 {
		return coldata.ZeroBatch
	}
	peersCol := batch.ColVec(r.peersColIdx).Bool()
	rankVec := batch.ColVec(r.outputColIdx)
	rankCol := rankVec.Int64()
	sel := batch.Selection()
	if sel != nil {
		for _, i := range sel[:n] {
			if peersCol[i] {
				r.rank++
				rankCol[i] = r.rank
			} else {
				rankCol[i] = r.rank

			}
		}
	} else {
		_ = peersCol[n-1]
		_ = rankCol[n-1]
		for i := 0; i < n; i++ {
			//gcassert:bce
			if peersCol[i] {
				r.rank++
				//gcassert:bce
				rankCol[i] = r.rank
			} else {
				//gcassert:bce
				rankCol[i] = r.rank

			}
		}
	}
	return batch
}

type denseRankWithPartitionOp struct {
	rankInitFields

	// rank indicates which rank should be assigned to the next tuple.
	rank int64
	// rankIncrement indicates by how much rank should be incremented when a
	// tuple distinct from the previous one on the ordering columns is seen.
	rankIncrement int64
}

var _ colexecop.Operator = &denseRankWithPartitionOp{}

func (r *denseRankWithPartitionOp) Init(ctx context.Context) {
	if !r.InitHelper.Init(ctx) {
		return
	}
	r.Input.Init(r.Ctx)
	// All rank functions start counting from 1. Before we assign the rank to a
	// tuple in the batch, we first increment r.rank, so setting this
	// rankIncrement to 1 will update r.rank to 1 on the very first tuple (as
	// desired).
	r.rankIncrement = 1
}

func (r *denseRankWithPartitionOp) Next() coldata.Batch {
	batch := r.Input.Next()
	n := batch.Length()
	if n == 0 {
		return coldata.ZeroBatch
	}
	partitionCol := batch.ColVec(r.partitionColIdx).Bool()
	peersCol := batch.ColVec(r.peersColIdx).Bool()
	rankVec := batch.ColVec(r.outputColIdx)
	rankCol := rankVec.Int64()
	sel := batch.Selection()
	if sel != nil {
		for _, i := range sel[:n] {
			if partitionCol[i] {
				// We need to reset the internal state because of the new partition.
				// Note that the beginning of new partition necessarily starts a new
				// peer group, so peersCol[i] *must* be true, and we will correctly
				// update the rank before setting it to rankCol.
				r.rank = 0
				r.rankIncrement = 1
			}
			if peersCol[i] {
				r.rank++
				rankCol[i] = r.rank
			} else {
				rankCol[i] = r.rank

			}
		}
	} else {
		_ = partitionCol[n-1]
		_ = peersCol[n-1]
		_ = rankCol[n-1]
		for i := 0; i < n; i++ {
			//gcassert:bce
			if partitionCol[i] {
				// We need to reset the internal state because of the new partition.
				// Note that the beginning of new partition necessarily starts a new
				// peer group, so peersCol[i] *must* be true, and we will correctly
				// update the rank before setting it to rankCol.
				r.rank = 0
				r.rankIncrement = 1
			}
			//gcassert:bce
			if peersCol[i] {
				r.rank++
				//gcassert:bce
				rankCol[i] = r.rank
			} else {
				//gcassert:bce
				rankCol[i] = r.rank

			}
		}
	}
	return batch
}
