package transactionpool

import (
	"github.com/NebulousLabs/Sia/consensus"
	"github.com/NebulousLabs/Sia/crypto"
)

func (tp *TransactionPool) removeUnconfirmedTransaction(ut *unconfirmedTransaction) {

	// after removing the unconfirmed transaction, try to throw it throught the
	// acceptance process, it's potentially still valid after being replaced by
	// a superset transaction.
}

func (tp *TransactionPool) removeDependentTransactions(t consensus.Transaction) {
}

func (tp *TransactionPool) confirmTransaction(t consensus.Transaction) {
	for _, sci := range t.SiacoinInputs {
		delete(tp.usedSiacoinOutputs, sci.ParentID)
	}
	for i, _ := range t.SiacoinOutputs {
		scoid := t.SiacoinOutputID(i)
		delete(tp.siacoinOutputs, scoid)
		delete(tp.newSiacoinOutputs, scoid)
	}
	for i, fc := range t.FileContracts {
		fcid := t.FileContractID(i)
		delete(tp.fileContracts, fcid)
		delete(tp.newFileContracts[fc.Start], fcid)
	}
	for _, fct := range t.FileContractTerminations {
		delete(tp.fileContractTerminations, fct.ParentID)
	}
	for _, sp := range t.StorageProofs {
		fc, _ := tp.state.FileContract(sp.ParentID)
		triggerBlock, _ := tp.state.BlockAtHeight(fc.Start)
		delete(tp.storageProofs[triggerBlock.ID()], sp.ParentID)
	}
	for _, sfi := range t.SiafundInputs {
		delete(tp.usedSiafundOutputs, sfi.ParentID)
	}
	for i, _ := range t.SiafundOutputs {
		sfoid := t.SiafundOutputID(i)
		delete(tp.siafundOutputs, sfoid)
		delete(tp.newSiafundOutputs, sfoid)
	}
	delete(tp.transactions, crypto.HashObject(t))
}

func (tp *TransactionPool) removeConflictingTransactions(t consensus.Transaction) {
}

func (tp *TransactionPool) update() {
	tp.state.RLock()
	defer tp.state.RUnlock()

	// Get the block diffs since the previous update.
	removedBlocksIDs, addedBlocksIDs, err := tp.state.BlocksSince(tp.recentBlock)
	if err != nil {
		if consensus.DEBUG {
			panic("BlocksSince returned an error?")
		}
		return
	}
	var removedBlocks, addedBlocks []consensus.Block
	for _, id := range removedBlocksIDs {
		block, exists := tp.state.Block(id)
		if !exists {
			if consensus.DEBUG {
				panic("state returned a block that doesn't exist?")
			}
			return
		}
		removedBlocks = append(removedBlocks, block)
	}
	for _, id := range addedBlocksIDs {
		block, exists := tp.state.Block(id)
		if !exists {
			if consensus.DEBUG {
				panic("state returned a block that doesn't exist?")
			}
			return
		}
		addedBlocks = append(addedBlocks, block)
	}

	// Add all of the removed transactions into the linked list.
	for _, block := range removedBlocks {
		// TODO: Check if any storage proofs have been invalidated.

		for j := len(block.Transactions) - 1; j >= 0; j-- {
			txn := block.Transactions[j]

			// If the transaction contains a storage proof or is non-standard,
			// remove this transaction from the pool. This is done last because
			// we also need to remove any dependents.
			err = tp.IsStandardTransaction(txn)
			if err != nil {
				tp.removeDependentTransactions(txn)
			}

			ut := &unconfirmedTransaction{
				transaction: txn,
				dependents:  make(map[*unconfirmedTransaction]struct{}),
			}

			tp.applySiacoinInputs(txn, ut)
			tp.applySiacoinOutputs(txn, ut)
			tp.applyFileContracts(txn, ut)
			tp.applyFileContractTerminations(txn, ut)
			tp.applyStorageProofs(txn, ut)
			tp.applySiafundInputs(txn, ut)
			tp.applySiafundOutputs(txn, ut)

			// Add the transaction to the front of the linked list.
			tp.prependUnconfirmedTransaction(ut)
		}
	}

	// Once moving forward again, remove any conflicts in the linked list that
	// occur with transactions that got accepted.
	for _, block := range addedBlocks {
		for _, txn := range block.Transactions {
			// Determine if this transaction is in the unconfirmed set or not.
			_, exists := tp.transactions[crypto.HashObject(txn)]
			if exists {
				tp.confirmTransaction(txn)
			} else {
				tp.removeConflictingTransactions(txn)
			}
		}
	}

	// TODO: Check if any file contracts have been invalidated.

	tp.recentBlock = tp.state.CurrentBlock().ID()
}
