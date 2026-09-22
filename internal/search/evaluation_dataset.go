package search

// EvalQuery is a single retrieval evaluation case.
// Query is the input text issued to a retrieval strategy.
// ExpectedIDs lists the question IDs considered relevant for the query.
// Category is the canonical topic label used to slice metrics by topic.
// IDs are stable synthetic fixtures (q-<slug>-NNN) so evaluation tests can
// seed mock repositories without a database.
type EvalQuery struct {
	Query       string   `json:"query"`
	ExpectedIDs []string `json:"expected_ids"`
	Category    string   `json:"category"`
}

// EvaluationDataset contains 60 queries (12 categories x 5 queries) covering
// the core OS topics used by the seed data and PLAN4 Phase 17 examples.
// Phrasing mixes exact keyword queries ("paging questions"), natural language
// ("what is virtual memory"), and semantic paraphrases ("how does the OS
// decide which process runs next") so vector, keyword, and hybrid strategies
// can be compared fairly.
var EvaluationDataset = []EvalQuery{
	// Deadlock (5)
	{Query: "deadlock questions", ExpectedIDs: []string{"q-deadlock-001", "q-deadlock-002", "q-deadlock-003"}, Category: "Deadlock"},
	{Query: "questions about circular wait", ExpectedIDs: []string{"q-deadlock-002", "q-deadlock-004"}, Category: "Deadlock"},
	{Query: "explain deadlock prevention vs avoidance", ExpectedIDs: []string{"q-deadlock-001", "q-deadlock-003", "q-deadlock-005"}, Category: "Deadlock"},
	{Query: "what are the four necessary conditions for deadlock", ExpectedIDs: []string{"q-deadlock-001", "q-deadlock-004"}, Category: "Deadlock"},
	{Query: "deadlock detection and recovery methods", ExpectedIDs: []string{"q-deadlock-005", "q-deadlock-006"}, Category: "Deadlock"},

	// CPU Scheduling (5)
	{Query: "CPU scheduling algorithms", ExpectedIDs: []string{"q-cpusched-001", "q-cpusched-002", "q-cpusched-003"}, Category: "CPU Scheduling"},
	{Query: "how does the OS decide which process runs next", ExpectedIDs: []string{"q-cpusched-001", "q-cpusched-004"}, Category: "CPU Scheduling"},
	{Query: "round robin vs FCFS scheduling", ExpectedIDs: []string{"q-cpusched-002", "q-cpusched-003"}, Category: "CPU Scheduling"},
	{Query: "shortest job first scheduling advantages", ExpectedIDs: []string{"q-cpusched-003", "q-cpusched-005"}, Category: "CPU Scheduling"},
	{Query: "priority scheduling and starvation", ExpectedIDs: []string{"q-cpusched-004", "q-cpusched-006"}, Category: "CPU Scheduling"},

	// Paging (5)
	{Query: "paging questions", ExpectedIDs: []string{"q-paging-001", "q-paging-002", "q-paging-003"}, Category: "Paging"},
	{Query: "how does paging translate virtual to physical addresses", ExpectedIDs: []string{"q-paging-001", "q-paging-004"}, Category: "Paging"},
	{Query: "page table structure and multilevel paging", ExpectedIDs: []string{"q-paging-002", "q-paging-005"}, Category: "Paging"},
	{Query: "paging vs segmentation differences", ExpectedIDs: []string{"q-paging-003", "q-segment-001"}, Category: "Paging"},
	{Query: "page faults and demand paging", ExpectedIDs: []string{"q-paging-004", "q-paging-006"}, Category: "Paging"},

	// Virtual Memory (5)
	{Query: "virtual memory", ExpectedIDs: []string{"q-vmem-001", "q-vmem-002", "q-vmem-003"}, Category: "Virtual Memory"},
	{Query: "what is virtual memory and why is it used", ExpectedIDs: []string{"q-vmem-001", "q-vmem-004"}, Category: "Virtual Memory"},
	{Query: "demand paging in virtual memory systems", ExpectedIDs: []string{"q-vmem-002", "q-paging-004"}, Category: "Virtual Memory"},
	{Query: "page replacement algorithms LRU FIFO optimal", ExpectedIDs: []string{"q-vmem-003", "q-vmem-005"}, Category: "Virtual Memory"},
	{Query: "thrashing causes and working set model", ExpectedIDs: []string{"q-vmem-005", "q-vmem-006"}, Category: "Virtual Memory"},

	// Synchronization (5)
	{Query: "process synchronization problems", ExpectedIDs: []string{"q-sync-001", "q-sync-002", "q-sync-003"}, Category: "Synchronization"},
	{Query: "producer consumer problem with semaphores", ExpectedIDs: []string{"q-sync-001", "q-sync-004"}, Category: "Synchronization"},
	{Query: "readers writers problem solution", ExpectedIDs: []string{"q-sync-002", "q-sync-005"}, Category: "Synchronization"},
	{Query: "dining philosophers problem", ExpectedIDs: []string{"q-sync-003", "q-sync-006"}, Category: "Synchronization"},
	{Query: "critical section and mutual exclusion", ExpectedIDs: []string{"q-sync-004", "q-sync-005"}, Category: "Synchronization"},

	// Memory Management (5)
	{Query: "memory management techniques", ExpectedIDs: []string{"q-memmgmt-001", "q-memmgmt-002", "q-memmgmt-003"}, Category: "Memory Management"},
	{Query: "contiguous memory allocation fragmentation", ExpectedIDs: []string{"q-memmgmt-001", "q-memmgmt-004"}, Category: "Memory Management"},
	{Query: "first fit best fit worst fit allocation", ExpectedIDs: []string{"q-memmgmt-002", "q-memmgmt-005"}, Category: "Memory Management"},
	{Query: "compaction and external fragmentation", ExpectedIDs: []string{"q-memmgmt-004", "q-memmgmt-006"}, Category: "Memory Management"},
	{Query: "swapping and memory allocation strategies", ExpectedIDs: []string{"q-memmgmt-003", "q-memmgmt-006"}, Category: "Memory Management"},

	// Segmentation (5)
	{Query: "segmentation in operating systems", ExpectedIDs: []string{"q-segment-001", "q-segment-002", "q-segment-003"}, Category: "Segmentation"},
	{Query: "segment table base and limit registers", ExpectedIDs: []string{"q-segment-001", "q-segment-004"}, Category: "Segmentation"},
	{Query: "segmented paging combined scheme", ExpectedIDs: []string{"q-segment-002", "q-paging-002"}, Category: "Segmentation"},
	{Query: "advantages of segmentation over paging", ExpectedIDs: []string{"q-segment-003", "q-paging-003"}, Category: "Segmentation"},
	{Query: "protection and sharing with segments", ExpectedIDs: []string{"q-segment-004", "q-segment-005"}, Category: "Segmentation"},

	// File Systems (5)
	{Query: "file system implementation questions", ExpectedIDs: []string{"q-filesys-001", "q-filesys-002", "q-filesys-003"}, Category: "File Systems"},
	{Query: "file allocation methods contiguous linked indexed", ExpectedIDs: []string{"q-filesys-001", "q-filesys-004"}, Category: "File Systems"},
	{Query: "directory structure and file operations", ExpectedIDs: []string{"q-filesys-002", "q-filesys-005"}, Category: "File Systems"},
	{Query: "inode structure in Unix file systems", ExpectedIDs: []string{"q-filesys-003", "q-filesys-006"}, Category: "File Systems"},
	{Query: "disk scheduling and free space management", ExpectedIDs: []string{"q-filesys-004", "q-filesys-005"}, Category: "File Systems"},

	// Processes (5)
	{Query: "process lifecycle and states", ExpectedIDs: []string{"q-process-001", "q-process-002", "q-process-003"}, Category: "Processes"},
	{Query: "process control block contents", ExpectedIDs: []string{"q-process-001", "q-process-004"}, Category: "Processes"},
	{Query: "context switching between processes", ExpectedIDs: []string{"q-process-002", "q-process-005"}, Category: "Processes"},
	{Query: "fork exec and process creation", ExpectedIDs: []string{"q-process-003", "q-process-006"}, Category: "Processes"},
	{Query: "interprocess communication pipes and message passing", ExpectedIDs: []string{"q-process-004", "q-process-005"}, Category: "Processes"},

	// Threads (5)
	{Query: "threads vs processes", ExpectedIDs: []string{"q-thread-001", "q-thread-002", "q-process-001"}, Category: "Threads"},
	{Query: "user level vs kernel level threads", ExpectedIDs: []string{"q-thread-001", "q-thread-003"}, Category: "Threads"},
	{Query: "multithreading models many to one one to one", ExpectedIDs: []string{"q-thread-002", "q-thread-004"}, Category: "Threads"},
	{Query: "thread synchronization and thread pools", ExpectedIDs: []string{"q-thread-003", "q-sync-004"}, Category: "Threads"},
	{Query: "benefits of multithreaded programming", ExpectedIDs: []string{"q-thread-004", "q-thread-005"}, Category: "Threads"},

	// I/O (5)
	{Query: "I/O management and device drivers", ExpectedIDs: []string{"q-io-001", "q-io-002", "q-io-003"}, Category: "I/O"},
	{Query: "interrupt driven I/O vs polling", ExpectedIDs: []string{"q-io-001", "q-io-004"}, Category: "I/O"},
	{Query: "direct memory access DMA controller", ExpectedIDs: []string{"q-io-002", "q-io-005"}, Category: "I/O"},
	{Query: "blocking vs nonblocking I/O operations", ExpectedIDs: []string{"q-io-003", "q-io-006"}, Category: "I/O"},
	{Query: "disk I/O performance and buffering", ExpectedIDs: []string{"q-io-004", "q-io-005"}, Category: "I/O"},

	// Banker's Algorithm (5)
	{Query: "Banker's algorithm", ExpectedIDs: []string{"q-banker-001", "q-banker-002", "q-banker-003"}, Category: "Banker's Algorithm"},
	{Query: "safe state check with Banker's algorithm example", ExpectedIDs: []string{"q-banker-001", "q-banker-004"}, Category: "Banker's Algorithm"},
	{Query: "resource allocation graph and safe sequence", ExpectedIDs: []string{"q-banker-002", "q-banker-005"}, Category: "Banker's Algorithm"},
	{Query: "deadlock avoidance using Banker's algorithm", ExpectedIDs: []string{"q-banker-003", "q-deadlock-003"}, Category: "Banker's Algorithm"},
	{Query: "need matrix calculation in Banker's algorithm", ExpectedIDs: []string{"q-banker-004", "q-banker-005"}, Category: "Banker's Algorithm"},
}

// EvaluationCategories returns the distinct categories in dataset order.
func EvaluationCategories() []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, q := range EvaluationDataset {
		if _, ok := seen[q.Category]; ok {
			continue
		}
		seen[q.Category] = struct{}{}
		out = append(out, q.Category)
	}
	return out
}
