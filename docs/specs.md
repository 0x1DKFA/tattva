# AI-Assisted Engineering Self-Development Platform

## Product & Technical Specification — v0.1

### Working description

A local-first application that uses AI to turn complex software systems, technologies, and engineering concepts into **progressive, hands-on learning projects**.

The user selects something they want to understand or build — for example:

* a BitTorrent client
* Redis
* Git
* Docker
* a message queue
* a browser
* an LLM agent
* Blender
* a database
* a distributed key-value store

The system analyses the target, identifies its important concepts and architectural components, and produces a **curriculum of small, progressively harder implementation steps**.

The user then builds the system themselves.

AI acts as:

* curriculum designer
* teacher
* explainer
* hint provider
* code reviewer
* verifier
* progress tracker
* learning coach

AI should **not** primarily act as the person writing the software.

---

# 1. Product thesis

## Problem

AI-assisted software development has dramatically reduced the effort required to produce working software.

A developer can increasingly say:

> "Build X."

and receive a working implementation.

The resulting problem is that the process of producing software and the process of **understanding software** are becoming increasingly disconnected.

A developer may be able to use:

* databases
* distributed systems
* message queues
* networking libraries
* AI agents
* cloud infrastructure
* frameworks

without developing a strong mental model of what happens underneath them.

This creates a gap between:

> "I can build something using this."

and:

> "I understand how this works well enough to build a simplified version myself."

The product exists to address that gap.

---

# 2. Core philosophy

## Primary principle

> **AI should accelerate human development, not replace it.**

The objective is not to minimize the amount of work the user performs.

The objective is to maximize:

* understanding
* retention
* practical experience
* ability to reason about systems
* ability to debug
* ability to modify systems
* ability to build from first principles

The product should deliberately preserve some productive struggle.

---

## Learning over completion

Traditional AI coding tools optimize roughly for:

```text
Question
   ↓
AI
   ↓
Working code
```

This product optimizes for:

```text
Goal
   ↓
Understanding
   ↓
Small challenge
   ↓
Human implementation
   ↓
Feedback
   ↓
Understanding
   ↓
Next challenge
   ↓
Skill
```

Completion is useful, but it is not the primary outcome.

---

# 3. Product goals

## Goal 1 — Turn complex systems into buildable learning paths

Given a target system, produce a sequence of tasks that gradually builds toward a simplified implementation.

Example:

```text
Build a BitTorrent client

Phase 1
  TCP connection

Phase 2
  Protocol handshake

Phase 3
  Peer messages

Phase 4
  Torrent metadata

Phase 5
  Tracker

Phase 6
  Piece downloading

Phase 7
  File assembly
```

Each phase is decomposed into smaller conceptual steps.

---

## Goal 2 — Teach the concepts behind the implementation

Every step should answer:

* What are we building?
* Why are we building it?
* What concept does this teach?
* Why does this concept matter?
* How does this connect to the larger system?
* What prerequisite knowledge is assumed?
* What becomes possible after completing this step?

---

## Goal 3 — Make the user implement the system

The default experience should encourage the user to write the implementation.

AI may provide:

* explanations
* hints
* examples
* debugging assistance
* partial guidance
* code review

But the user remains responsible for the implementation.

---

## Goal 4 — Build and maintain a personal engineering roadmap

The application should allow users to establish long-term goals such as:

```text
Understand networking
Become stronger at distributed systems
Learn operating systems
Understand databases
Learn graphics programming
Become better at Go internals
```

Projects become vehicles for achieving those goals.

---

## Goal 5 — Track demonstrated understanding, not just task completion

The system should eventually distinguish between:

```text
Completed task
```

and:

```text
Demonstrated understanding
```

A user may complete a task with substantial AI assistance but still need more practice before the concept is considered understood.

---

# 4. Non-goals

The product is not primarily:

### A code generator

It should not compete directly with coding agents whose main objective is:

> "Give me the implementation."

### A conventional online course platform

The content is generated around the user's selected target rather than being a fixed course catalog.

### A generic project-management application

Progress tracking exists to support learning rather than project management.

### A benchmark for programmers

The goal is not to rank users against one another.

### A production clone generator

The objective is normally to build a **minimal, educational version** of a system, not reproduce every production feature.

---

# 5. Core user journey

The basic workflow is:

```text
Create Project
      ↓
Choose Target
      ↓
Analyse Target
      ↓
Generate Architecture
      ↓
Generate Curriculum
      ↓
Review Curriculum
      ↓
Start Step
      ↓
Understand Goal
      ↓
Implement
      ↓
Verify
      ↓
Reflect / Explain
      ↓
Complete
      ↓
Unlock Next Step
```

The loop then repeats.

---

# 6. Project types

The system should support several sources of truth.

## Type A — Existing repository

Example:

```text
https://github.com/example/project
```

The AI can analyse:

* repository structure
* source code
* tests
* documentation
* configuration
* dependencies
* architecture
* entry points

The user then creates a simplified version.

---

## Type B — Local application

The user points the application at a local directory.

Example:

```text
~/code/my-project
```

The AI analyses the local codebase.

This could be useful for understanding:

> "I work on this system every day, but I don't fully understand how it works."

---

## Type C — Publicly documented system

Example:

```text
"Build a miniature Kubernetes"
```

There may not be a single repository used as the authoritative source.

The AI instead constructs an architecture from:

* documentation
* specifications
* public source code
* known architecture
* user-provided requirements

---

## Type D — Natural-language concept

Example:

> "I want to understand how operating systems work."

The system constructs a learning project from the conceptual architecture.

For example:

```text
Boot process
   ↓
Memory
   ↓
Processes
   ↓
Scheduling
   ↓
System calls
   ↓
Filesystem
   ↓
Networking
```

---

# 7. Definition of a "miniature implementation"

The application should explicitly distinguish between:

> Production system

and:

> Educational implementation.

The goal is to identify the **minimum interesting subset** that demonstrates the underlying ideas.

For example:

### Miniature Kubernetes

The objective might be:

```text
CLI
 ↓
API server
 ↓
desired state
 ↓
scheduler
 ↓
worker
 ↓
container/process
```

rather than implementing the entire Kubernetes ecosystem.

The AI should optimize for:

> Maximum conceptual coverage with minimum implementation complexity.

---

# 8. Learning model

The fundamental hierarchy is:

```text
Goal
 ↓
Project
 ↓
Phase
 ↓
Concept
 ↓
Step
 ↓
Implementation
 ↓
Verification
 ↓
Reflection
```

---

# 9. Project

A project represents a long-running learning objective.

Example:

```text
Project

Name:
Build a Miniature Redis

Target:
Redis

Language:
Go

Goal:
Understand how an in-memory networked datastore works.

Status:
In progress

Progress:
42%

Started:
2026-09-01
```

Potential fields:

```text
id
name
description
target
source
language
difficulty
status
created_at
updated_at
```

---

# 10. Architecture model

Before generating the curriculum, the AI should produce an architecture model.

Example:

```text
Mini Redis

                 ┌──────────────┐
                 │    Client    │
                 └──────┬───────┘
                        │
                        ▼
                 ┌──────────────┐
                 │ TCP Server   │
                 └──────┬───────┘
                        │
                        ▼
                 ┌──────────────┐
                 │ Command      │
                 │ Parser       │
                 └──────┬───────┘
                        │
                        ▼
                 ┌──────────────┐
                 │ Data Store   │
                 └──────────────┘
```

The architecture should identify:

* components
* responsibilities
* relationships
* protocols
* dependencies
* important abstractions
* external interfaces

---

# 11. Curriculum

The curriculum is the ordered learning path.

It should not simply be a list.

It should contain dependencies.

Example:

```text
TCP
 ↓
Connection handling
 ↓
Protocol framing
 ↓
Command parsing
 ↓
Command execution
 ↓
Persistence
```

But some concepts may be parallel:

```text
             ┌── Networking ──┐
Core model ──┤                 ├── Server
             └── Serialization┘
```

Therefore internally the curriculum should be representable as a DAG.

---

# 12. Phase

A phase groups related concepts.

Example:

```text
Phase 2 — Networking

Purpose:
Understand how clients communicate with the server.

Concepts:
- TCP
- sockets
- connection lifecycle
- request/response protocols

Steps:
1. Open a TCP connection
2. Accept connections
3. Read bytes
4. Write responses
5. Handle multiple clients
```

---

# 13. Step

A step is the fundamental unit of work.

The guiding principle is:

> **One conceptual leap per step.**

A step should ideally be completable without requiring the user to understand several unrelated new concepts simultaneously.

---

# 14. Step specification

Each step should contain:

```text
ID

Title

Goal

Why

Context

Concepts

Prerequisites

Task

Constraints

Hints

Verification

Expected outcome

Common mistakes

Reflection question

Unlocks
```

Example:

```text
Step 12

Title:
Implement Message Framing

Goal:
Read exactly one protocol message from a TCP stream.

Why:
TCP provides a stream of bytes rather than message boundaries.

Concepts:
- TCP streams
- framing
- partial reads

Prerequisites:
- TCP connection
- basic byte handling

Task:
Implement a function that reads a complete
length-prefixed message.

Constraints:
- standard library only
- handle partial reads
- reject malformed messages

Verification:
Automated protocol tests

Reflection:
Why can't one Read() call be assumed to return
an entire message?

Unlocks:
- command parser
- request handling
```

---

# 15. Assistance model

AI assistance should be progressive.

Suggested assistance ladder:

```text
Level 0
No assistance

Level 1
Clarifying question

Level 2
Conceptual explanation

Level 3
Hint

Level 4
Pseudocode

Level 5
Partial implementation

Level 6
Full implementation
```

The system should default to the lowest useful level.

The user can explicitly escalate.

---

# 16. Example interaction

User:

> My TCP server sometimes receives incomplete messages.

Instead of immediately producing code:

```text
AI:

Let's reason about the underlying issue.

TCP is a byte stream rather than a message-oriented
protocol.

What assumption is your current implementation making
about Read()?
```

If the user is still stuck:

```text
Hint:

A single Read() call is allowed to return fewer bytes
than you requested.
```

Then:

```text
Stronger hint:

You probably need a mechanism that keeps reading until
the complete message has been received.
```

Eventually:

```text
Show me an implementation
```

can reveal an implementation.

---

# 17. Verification

A completed step should ideally be verifiable.

Verification types may include:

### Automated tests

```text
go test ./...
```

### Integration tests

```text
start server
send request
inspect response
```

### Protocol compatibility

```text
communicate with real Redis client
communicate with BitTorrent peer
```

### Behavioural checks

```text
input → expected output
```

### Explanation checks

Ask the user:

> Explain why this works.

AI evaluates the explanation.

### Design checks

Ask:

> What would happen if 10,000 clients connected simultaneously?

The objective is to test whether the user can reason beyond the implementation.

---

# 18. Understanding model

Eventually a concept should have its own learning state.

Example:

```text
Concept: TCP streams

Exposure:
3 projects

Implemented:
4 times

Explained:
2 times

Debugged:
3 times

Confidence:
Developing

Last practiced:
2026-09-20
```

A concept can therefore exist across projects.

For example:

```text
TCP streams

Build Redis
   ✓

Build BitTorrent
   ✓

Build HTTP server
   ✓

Build distributed KV store
   →
```

This creates a reusable knowledge graph.

---

# 19. Progress tracking

Project progress should include more than a percentage.

Example:

```text
Mini Redis

Implementation
██████████████░░░░░░ 70%

Concept coverage
████████████░░░░░░░░ 60%

Independent implementation
█████████░░░░░░░░░░░ 45%

Understanding checks
██████████░░░░░░░░░░ 50%
```

The exact formula can be introduced later.

The MVP can initially track only step completion.

---

# 20. Long-term goals

Users should be able to define development goals.

Example:

```text
Goal:

Become stronger at distributed systems
```

The system maps that goal to concepts:

```text
Networking
Concurrency
Consistency
Replication
Fault tolerance
Consensus
Distributed storage
Observability
```

And then to projects:

```text
Build HTTP server
Build message queue
Build Redis
Build replicated KV store
Build Raft
Build distributed scheduler
```

This produces a personal learning roadmap.

---

# 21. Personal knowledge graph

Long term, the system should know:

```text
User
 │
 ├── Goals
 │
 ├── Projects
 │
 ├── Concepts
 │      │
 │      ├── learned through project A
 │      ├── practiced through project B
 │      └── weak in project C
 │
 └── Skills
```

This allows the system to identify gaps.

Example:

> You've implemented networking several times, but most of your projects have avoided concurrency. The next project could deliberately exercise concurrent systems design.

The product is therefore not just a curriculum generator.

It becomes a **personal engineering development system**.

---

# 22. Reflection

Every major phase should eventually contain reflection.

Example:

```text
You just implemented Redis pipelining.

Explain:

1. Why does pipelining improve throughput?
2. What problem does it avoid?
3. What trade-off does it introduce?
4. How would you debug a pipeline implementation?
```

The goal is to transition from:

> "I followed the instructions."

to:

> "I understand the underlying mechanism."

---

# 23. AI analysis pipeline

The AI analysis should be multi-stage rather than one giant prompt.

## Stage 1 — Reconnaissance

Determine:

* purpose
* major features
* entry points
* architecture
* external interfaces
* dependencies
* important subsystems

---

## Stage 2 — Concept extraction

Identify the concepts required to understand the system.

Example:

```text
TCP
binary protocols
hashing
concurrency
file IO
peer discovery
```

---

## Stage 3 — Architecture reconstruction

Construct:

* component graph
* dependency graph
* data flow
* control flow
* important interfaces

---

## Stage 4 — Educational simplification

Determine:

> What is the smallest implementation that still teaches the essential concepts?

This is critical.

The AI should deliberately remove production complexity where appropriate.

---

## Stage 5 — Curriculum generation

Transform the architecture into:

```text
phases
 ↓
milestones
 ↓
steps
```

---

## Stage 6 — Step decomposition

Ensure each step introduces a manageable conceptual change.

The AI should detect steps that are too large and split them.

---

## Stage 7 — Verification generation

For each step, generate possible verification mechanisms.

---

## Stage 8 — Teaching material

Generate:

* explanation
* goal
* context
* hints
* common mistakes
* reflection questions

---

# 24. Learning Specification

Claude should not directly generate the UI.

Instead, the AI should produce a structured intermediate representation.

Example:

```yaml
project:
  name: Mini Redis
  language: go

architecture:
  components:
    - id: tcp-server
      description: Accept client connections

    - id: protocol-parser
      description: Parse Redis requests

    - id: datastore
      description: Store key/value pairs

concepts:
  - tcp
  - streams
  - protocols
  - parsing
  - concurrency
  - memory

phases:

  - id: networking
    title: Networking

steps:

  - id: networking-01
    title: Open a TCP connection

    prerequisites: []

    concepts:
      - tcp

    task:
      type: implementation

    verification:
      type: automated-test
```

This specification becomes the contract between:

```text
AI
```

and:

```text
Application
```

The application owns the resulting curriculum after generation.

---

# 25. Claude Code integration

The initial application should treat Claude Code as an external agent.

Conceptually:

```text
Local Application
       │
       ▼
Claude Adapter
       │
       ▼
Claude Code
       │
 ┌─────┴────────┐
 │              │
Repository   Documentation
 │              │
 └──────┬───────┘
        ▼
   Analysis
        ▼
Learning Specification
```

The application should not tightly couple the entire product to Claude.

Future adapters could support other models or agents.

---

# 26. Local-first architecture

The initial version should be local-first.

Proposed conceptual architecture:

```text
┌──────────────────────────────────────┐
│                UI                    │
│                                      │
│ Projects / Curriculum / Steps       │
│ Progress / Goals / Learning          │
└──────────────────┬───────────────────┘
                   │
                   ▼
┌──────────────────────────────────────┐
│             Application API          │
│                                      │
│ Project Service                      │
│ Curriculum Service                   │
│ Progress Service                     │
│ Verification Service                │
│ AI/Agent Service                     │
└──────────┬───────────────┬───────────┘
           │               │
           ▼               ▼
       SQLite          Claude Code
           │               │
           │               ▼
           │          Target Repository
           │
           ▼
        Projects
        Progress
        Knowledge
```

SQLite is sufficient for the initial implementation.

---

# 27. Local project interaction

The application may eventually run commands against the user's project.

Potential capabilities:

```text
run tests
run build
run linters
inspect git diff
inspect source files
run custom verification
```

This needs to be treated as a security boundary.

The user must explicitly control what the application is allowed to execute.

---

# 28. Git integration

Git should eventually be first-class.

Possible behaviour:

```text
Step 14 started
       ↓
User modifies repository
       ↓
Application detects changes
       ↓
Tests run
       ↓
AI reviews implementation
       ↓
Step completed
```

Git history could also provide learning history:

```text
Step 14
3 commits
47 minutes
2 failed tests
1 hint requested
```

This is much more useful than simply:

```text
✓ completed
```

---

# 29. UI

The UI should remain intentionally simple.

## Home

```text
My Development

Goals
Projects
Recent activity
Current focus
```

---

## Project page

```text
Mini Redis

Progress
██████████░░░░ 57%

Architecture

Curriculum

Concepts

Activity
```

---

## Curriculum page

```text
Phase 1 — Networking
 ✓ Open TCP connection
 ✓ Accept connections
 ✓ Read bytes

Phase 2 — Protocol
 ✓ Message framing
 → Parse requests
 ○ Encode responses
 ○ Handle errors

Phase 3 — Storage
 ○ In-memory store
 ○ GET
 ○ SET
 ○ DELETE
```

---

## Step page

```text
Step 14

Implement Message Parsing

Goal
Understand protocol parsing.

Why
The server receives raw bytes and must transform
them into structured commands.

Concepts
TCP
Framing
Parsing

Your task
...

Hints
[Need help?]

Verification
[Run verification]

Reflection
[Explain what you learned]
```

---

# 30. AI interaction modes

The application should have several distinct AI actions rather than one generic chat box.

Possible actions:

```text
Explain
Hint
Why?
Review my implementation
Help me debug
Explain the architecture
Quiz me
Challenge my understanding
Show an example
Reveal solution
```

This is preferable to a generic:

```text
Chat with AI
```

because each interaction has a pedagogical purpose.

---

# 31. "Why?" mode

Every important concept should support a contextual explanation.

Example:

```text
Why do we need a tracker?

Because a BitTorrent client needs peers to communicate
with.

At this point your client knows how to communicate with
a peer, but it doesn't yet know which peers exist.

The tracker solves the peer-discovery problem.

Current dependency:

Torrent metadata
      ↓
Tracker
      ↓
Peers
      ↓
TCP connections
```

---

# 32. "What happens if I skip this?"

The system should explain dependencies.

Example:

```text
What happens if I skip message framing?

The following steps all depend on it:

- request parsing
- command execution
- response generation

You could implement them independently, but would
duplicate message-boundary handling in several places.
```

This reinforces architecture.

---

# 33. Assessment model

The eventual product can classify each step as:

```text
Not started
Started
Assisted
Completed
Demonstrated
Mastered
```

But these labels should not initially be treated as objective measurements.

They are signals derived from evidence such as:

* implementation
* test success
* explanations
* debugging
* repeated practice
* independent completion

---

# 34. Anti-cheating philosophy

The product should not try to prevent users from asking AI for answers.

Instead, it should make the consequences visible.

For example:

```text
You requested the full implementation.

This step is marked:
Completed with full assistance.

You may want to revisit it later and implement
it independently.
```

This preserves user agency.

The objective is self-development, not policing.

---

# 35. MVP

The first version should be substantially smaller than the complete vision.

## MVP goal

Support:

> Given a repository or description, generate a structured learning curriculum and guide the user through it step-by-step while tracking progress.

### MVP features

```text
✓ Create project
✓ Point at local repository
✓ Provide natural-language target
✓ Run Claude Code analysis
✓ Generate architecture
✓ Generate curriculum
✓ Review curriculum
✓ Step-by-step UI
✓ Mark steps complete
✓ Run basic verification
✓ Track project progress
✓ Ask AI for hints/explanations
✓ Persist everything locally
```

---

# 36. MVP exclusions

Initially avoid:

```text
complex knowledge graph
social features
leaderboards
multi-user accounts
cloud sync
advanced gamification
automatic mastery scoring
large-scale analytics
marketplace of curricula
```

These can come later.

The first question to validate is simply:

> **Does turning a complex system into an interactive build path actually create a substantially better learning experience?**

---

# 37. Initial project examples

The MVP should use projects where the educational structure is clear.

Good candidates:

```text
Build Your Own Git
Build Your Own Redis
Build Your Own HTTP Server
Build Your Own Message Queue
Build Your Own BitTorrent Client
Build Your Own Shell
Build Your Own Database
Build Your Own Container Runtime
```

These systems have reasonably understandable boundaries and progressive implementations.

---

# 38. Success criteria

The first version should not be measured primarily by:

```text
AI response quality
number of generated projects
lines of code generated
```

Better signals are:

### Engagement

Do users actually return to unfinished projects?

### Completion

How many users complete meaningful sections?

### Retention

Do users continue using the system over weeks/months?

### Understanding

Can users explain concepts after completing them?

### Independence

Can users complete later steps with less assistance?

### Transfer

Can the user apply the concept in another project?

The strongest long-term signal is:

> **Does the user become less dependent on the AI for the same class of problem?**

That is almost the opposite of the traditional AI coding product metric.

---

# 39. Potential long-term product loop

Eventually:

```text
User sets goal
      ↓
AI proposes learning path
      ↓
User builds projects
      ↓
System observes behaviour
      ↓
Concepts become stronger
      ↓
AI identifies gaps
      ↓
Next project targets gaps
      ↓
User becomes more capable
      ↓
AI assistance becomes less necessary
```

The ideal outcome is paradoxical:

> **A successful user should eventually need less help from the product.**

That is a desirable property, not a failure mode.

---

# 40. Long-term vision

The eventual system becomes something like a personal engineering development environment.

```text
                    MY DEVELOPMENT
                           │
          ┌────────────────┼────────────────┐
          │                │                │
        Goals           Concepts         Skills
          │                │                │
          └────────────────┼────────────────┘
                           │
                        Projects
                           │
                         Steps
                           │
                     Implementations
                           │
                       Evidence
                           │
                      Understanding
```

A user could eventually say:

> "I want to become significantly better at distributed systems over the next six months."

The system could construct a sequence of increasingly difficult implementation projects, track the concepts encountered, identify weak areas, and continuously adjust the learning path.

The product would therefore sit somewhere between:

* an interactive learning platform
* a personal engineering roadmap
* a coding practice environment
* an AI tutor
* a project-based curriculum generator
* a development journal

but remain fundamentally centered on **self-directed engineering growth**.

---

# 41. Core product principle

Everything in the product should ultimately support this statement:

> **Don't use AI to avoid doing the thinking. Use AI to help you do better thinking.**

The application should help the user answer:

> "Could I explain this?"

> "Could I implement a small version of it?"

> "Could I debug it?"

> "Could I modify it?"

> "Could I build it again without the guide?"

Those are stronger measures of engineering capability than simply producing working code.

---

# 42. Initial technical recommendation

A pragmatic first implementation could be:

```text
Frontend:
Local web application

Backend:
Go

Database:
SQLite

Agent:
Claude Code CLI

Project execution:
Local subprocesses

Version control:
Git

Communication:
HTTP / local IPC
```

The precise framework is less important than keeping the architecture modular.

The most important boundary is:

```text
             ┌────────────────────┐
             │ Learning Platform  │
             └─────────┬──────────┘
                       │
              Learning Specification
                       │
             ┌─────────▼──────────┐
             │     AI Agent       │
             └────────────────────┘
```

This prevents the product from becoming merely a UI wrapper around Claude Code.

---

# 43. Open product decisions

These decisions should be discussed before the implementation architecture becomes too concrete.

## Decision 1 — What exactly counts as "understanding"?

There are at least three possible interpretations:

### A

The user successfully implements the step.

### B

The user implements it and can explain it.

### C

The user can implement, explain, debug, and transfer the concept to another context.

My recommendation for the long-term product model is C, but the MVP can start with A + lightweight reflection.

---

## Decision 2 — How much should AI be allowed to do?

Possible philosophy:

```text
AI strongly restricts direct solutions
```

versus:

```text
AI gives users complete freedom but tracks
how much assistance they used
```

The second is probably more consistent with the product's self-development philosophy.

---

## Decision 3 — How authoritative is the target implementation?

When analysing an existing project, should the curriculum attempt to teach:

```text
How the real system works
```

or:

```text
How to understand the core ideas represented
by the system
```

Those are not always identical.

For example, production systems contain enormous amounts of complexity that may not be educationally useful.

The product probably needs to explicitly separate:

```text
Real architecture
```

from:

```text
Educational architecture
```

---

## Decision 4 — Should the user be able to change the curriculum?

I think they should.

The AI should generate the initial path, but the user should be able to say:

```text
Make this harder.
Skip this topic.
Go deeper on networking.
Use Go instead of Rust.
Give me more exercises.
Explain the underlying protocol first.
```

The curriculum should be **adaptive rather than sacred**.

---

## Decision 5 — Is the primary unit the project or the person?

The project is the natural starting point, but the long-term architecture should probably treat the **person's development** as the top-level object.

That enables:

```text
Goal
 ↓
Concept gaps
 ↓
Projects
 ↓
Evidence
 ↓
Skill development
```

rather than isolated projects that never connect.

---

# 44. The product in one sentence

> **A local AI-assisted engineering development environment that turns software systems and engineering concepts into progressive, hands-on projects so developers can learn by building rather than simply asking AI to build for them.**

