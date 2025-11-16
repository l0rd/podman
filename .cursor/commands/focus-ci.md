Update Podman CI files (cirrus.yml and packit.yaml) to only run one test.

The test to run is passed as an argument to the command.

If the user doesn't provide an argument, ask them to provide one.

- Go over the file `.cirrus.yml` and comment out all the `skip:` field to
all tasks that have one. 
- Add `skip: true` to all tasks
- Switch to `skip: false` to the task that you want to run.
- Switch to `skip: false` to all the tasks that the test is dependent on.
- Go over the file `packit.yaml` and set `trigger: ignore` to all the jobs that
trigger `commit` and `pull_request`.
