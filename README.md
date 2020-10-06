# Distributed build system. 
The build system receives the build graph and source files as input. Build result
 are executable files and stderr / stdout of running processes


The assembly graph consists of jobs. Each job describes the commands that need to be run on one machine,
 along with all the input files these commands need to work.
  
 Jobs in the assembly graph run arbitrary commands. For example, call the compiler, linker or
 run tests.
  
 The commands inside the job can read files from the file system. We will distinguish between two kinds of files:
  - Files with source code from the user's machine.
  - Files that spawned other jobs.
  
 The commands inside the job can write the results of their work to files on the disk. Output files
 must be inside its output directory. The directory with the result of the job is called
 artifact.

## System architecture
  
 Our system will have three components.
  * Client is the process that starts the build.
  * Worker is a process that launches compilation and testing commands.
  * Coordinator - the central process in the system, communicates with clients and workers. Distributes tasks
    workers.
  
 A typical assembly looks like this:
 1. The client connects to the coordinator, sends him the assembly graph and input files for the assembly graph.
 2. The coordinator saves the assembly graph in memory and starts its execution.
 3. Workers start executing the vertices of the graph, sending each other output job directories.
 4. The results of the jobs are downloaded to the client.

## Usage

 For usage examples please refer to `disttest/three_workers_test.go` or `disttest/single_worker_test.go`
