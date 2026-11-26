package mobileapp

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// eventSeqHeader names the newest event an answer already holds (bc-sseq
// FR3): the hub's head seq, read before the answer loads anything. Every
// broadcast is made after its transaction commits, so an answer whose read
// began at seq N holds every change announced at or below N, and a page
// that applied it needs no refetch for those events.
const eventSeqHeader = "X-Event-Seq"

// headSeq is the seq of the newest event the hub has stamped, 0 before the
// first. A held match push takes its seq when it is sent, so a read made
// while one is held names a lower seq and the push still refetches.
func headSeq(h *Hub) int64 {
	return h.seq.Load()
}

// setEventSeq stamps an answer with the seq its read began at.
func setEventSeq(c *gin.Context, seq int64) {
	c.Header(eventSeqHeader, strconv.FormatInt(seq, 10))
}
