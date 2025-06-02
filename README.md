# quickly
Fast concurrent command runner, written in Go.

## Installation

Get the latest build from [Releases](https://github.com/joeyciechanowicz/quickly/releases) and place it in your path.

Alternatively install with `Go`

```sh
go install github.com/joeyciechanowicz/quickly@latest
```

## Usage

Run it once to create a `~/.quicklyrc` file, add directories you want to run commands in to that file. One directory per line. For example

```
~/git/some-dir
~/git/other-dir
```

### Git status

Quickly handles `status` itself to provide a quick overview of the git status of each directory. 

```sh
> quickly status
[first-project]           main            Clean 
[api-service]             main            1 modified 
[automation]              feat1           Clean [behind 1]
```

### Command runner

```sh
quickly some command
```

Or if you want a more complex command wrap it in quotes, as all commands are run in a shell.

```sh
quickly 'cat somefile.txt | grep something'
```


### Filter based on branch

If you want to only run commands when the branch matches, you can use `--if-branch`.

```sh
quickly --if-branch some-branch your command
```

You can also match substrings, or use the shorthand version
```sh
quickly -b som your command
```
