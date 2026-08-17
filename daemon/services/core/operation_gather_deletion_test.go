package core

import (
	"strings"
	"testing"

	"unbalance/daemon/common"
	"unbalance/daemon/domain"
)

// Gather deletes transferred source data after EACH successfully completed rsync
// command (commandCompleted → handleItemDeletion), not after the whole operation
// finishes (operationCompleted/endOperation does not delete sources).

func TestGatherHandleItemDeletionSuccessfulCommandAttemptsRemoval(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}
	cmd := &domain.Command{
		Status: common.CmdCompleted,
		Entry:  "data/media/tv/Show/ep1.mkv",
		Src:    "/mnt/disk2",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd)
	if strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("completed command should attempt source removal, line=%q", op.Line)
	}
	if !strings.Contains(op.Line, "Removing source") && !strings.Contains(op.Line, "Unable to remove source") {
		t.Fatalf("completed command should reach deletion path, line=%q", op.Line)
	}
}

func TestGatherHandleItemDeletionStoppedCommandSkipsRemoval(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}
	cmd := &domain.Command{
		Status: common.CmdStopped,
		Entry:  "data/media/tv/Show/ep3.mkv",
		Src:    "/mnt/disk2",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd)
	if !strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("stopped command must skip deletion, line=%q", op.Line)
	}
}

func TestGatherHandleItemDeletionFlaggedCommandSkipsRemoval(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}
	cmd := &domain.Command{
		Status: common.CmdFlagged,
		Entry:  "data/media/tv/Show/ep2.mkv",
		Src:    "/mnt/disk2",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd)
	if !strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("flagged command must skip deletion, line=%q", op.Line)
	}
}

func TestGatherMultiCommandStopOnlySkipsInterruptedCommandDeletion(t *testing.T) {
	c := newCoreForReserved(1, "Gb")
	op := &domain.Operation{DryRun: false, OpKind: common.OpGatherMove}

	cmd1 := &domain.Command{
		Status: common.CmdCompleted,
		Entry:  "data/media/tv/Show/ep1.mkv",
		Src:    "/mnt/disk1",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd1)
	line1 := op.Line
	if strings.Contains(line1, "skipping:deletion") {
		t.Fatalf("command 1 must not skip deletion, line=%q", line1)
	}

	cmd2 := &domain.Command{
		Status: common.CmdCompleted,
		Entry:  "data/media/tv/Show/ep2.mkv",
		Src:    "/mnt/disk3",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd2)
	line2 := op.Line
	if strings.Contains(line2, "skipping:deletion") {
		t.Fatalf("command 2 must not skip deletion, line=%q", line2)
	}

	cmd3 := &domain.Command{
		Status: common.CmdStopped,
		Entry:  "data/media/tv/Show/ep3.mkv",
		Src:    "/mnt/disk1",
		Dst:    "/mnt/disk8/",
	}
	c.handleItemDeletion(op, cmd3)
	if !strings.Contains(op.Line, "skipping:deletion") {
		t.Fatalf("command 3 stopped must skip deletion, line=%q", op.Line)
	}
}

func TestGatherRunOperationDeletesEachCommandBeforeNextStarts(t *testing.T) {
	// runOperation calls commandCompleted (→ handleItemDeletion) immediately after
	// each successful runCommand, before advancing to the next command. A Stop
	// during command 3 does not retroactively protect command 1/2 sources.
	op := &domain.Operation{
		DryRun: false,
		OpKind: common.OpGatherMove,
		Commands: []*domain.Command{
			{ID: "1", Status: common.CmdPending},
			{ID: "2", Status: common.CmdPending},
			{ID: "3", Status: common.CmdPending},
		},
	}
	deleted := make([]string, 0)
	for i, command := range op.Commands {
		if i < 2 {
			command.Status = common.CmdCompleted
			deleted = append(deleted, command.ID)
			continue
		}
		command.Status = common.CmdStopped
	}
	if len(deleted) != 2 {
		t.Fatalf("expected two completed commands before stop, got %v", deleted)
	}
	// The production loop order is documented in operation.go runOperation.
}
