package node

import (
	"context"
	"errors"

	"github.com/keep-network/keep-core/pkg/frost"
	"github.com/keep-network/keep-core/pkg/frost/dkg"
)

// readyAcceptance rechecks the exact public candidate and canonical approval
// retained by this process's journal. It cannot publish/approve a new candidate.
type readyAcceptance struct {
	chain      dkg.Chain
	parameters dkg.Parameters
	selection  dkg.Selection
	record     dkg.WalletRecord
}

func (a readyAcceptance) WaitCandidateAcceptance(ctx context.Context, c frost.Candidate) (frost.Receipt, error) {
	descriptor, err := dkg.NewDescriptor(a.parameters, a.selection, c)
	if err != nil {
		return frost.Receipt{}, err
	}
	hash, err := dkg.DescriptorHash(descriptor)
	if err != nil {
		return frost.Receipt{}, err
	}
	if hash != a.record.DescriptorHash || dkg.WalletID(c.OutputKey) != a.record.ID {
		return frost.Receipt{}, errors.New("reloaded candidate differs from durable approval")
	}
	view, err := a.chain.View(ctx, a.record.Descriptor.Epoch)
	if err != nil {
		return frost.Receipt{}, err
	}
	if view.ApprovedID != a.record.ID || view.Approved.State != 2 || view.ApprovalHash != a.record.ApprovalHash || view.Approved.DescriptorHash != a.record.DescriptorHash || view.Approved.ResultHash != a.record.ResultHash || view.Approved.ApprovalBlock != a.record.ApprovalBlock {
		return frost.Receipt{}, errors.New("reloaded candidate approval is no longer canonical and ready")
	}
	return frost.Receipt{Epoch: c.Epoch, Descriptor: c.Descriptor}, nil
}
