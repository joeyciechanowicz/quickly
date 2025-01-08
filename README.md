# quickly
Fast concurrent command runner, written in Go.

## Installation

Get the latest build from [Releases](https://github.com/joeyciechanowicz/quickly/releases) and place it in your path.

Alternatively install with `Go`

```sh
go install github.com/joeyciechanowicz/quickly@latest
```

## Usage

Run it once to create a `~/.quicklyrc` file, add directories you want to run commands in to that file. One directory per line.


### Git status

Quickly handles `status` itself to provide a quick overview of the git status of each directory. 

```sh
quickly status
```

### Command runner

```sh
quickly some command
```

or if you want a more complex command wrap it in quotes

```sh
quickly 'cat somefile.txt | grep something'
```
