# Installation Guide

This guide covers installing the ContextHelp CLI tools.

## Prerequisites

- Go version compatible with `go.mod` (use `mise install` if you follow the repo toolchain)
- Git (for cloning the repository)
- `task` (preferred task runner; if it is not on `PATH`, use `mise exec -- task ...`)

## Installation Methods

### Method 1: Build from Source (Recommended)

```bash
# Clone the repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Build both binaries
task build

# Binaries will be in bin/ctxt and bin/dpkms
```

### Method 2: Install to $GOPATH/bin

```bash
# Clone the repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Install to $GOPATH/bin
task install

# Make sure $GOPATH/bin is in your PATH
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Method 3: Manual Installation

```bash
# Clone the repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Build binaries
task build

# Copy to a directory in your PATH
sudo cp bin/ctxt /usr/local/bin/
sudo cp bin/dpkms /usr/local/bin/

# Or copy to user bin directory
mkdir -p ~/.local/bin
cp bin/ctxt ~/.local/bin/
cp bin/dpkms ~/.local/bin/

# Make sure ~/.local/bin is in your PATH
export PATH="$PATH:$HOME/.local/bin"
```

## Post-Installation Setup

### 1. Create Configuration Directory

```bash
mkdir -p ~/.config/contexthelp
```

### 2. Create Configuration File

```bash
# Copy example configuration
cp config/config.example.yaml ~/.config/contexthelp/config.yaml

# Edit configuration
$EDITOR ~/.config/contexthelp/config.yaml
```

### 3. Create Data Directory

```bash
mkdir -p ~/.local/share/contexthelp
```

### 4. Set Up Shell Completion (Optional)

#### Bash

```bash
# Add to ~/.bashrc or ~/.bash_profile
ctxt completion bash > ~/.bash_completion.d/ctxt
dpkms completion bash > ~/.bash_completion.d/dpkms
```

#### Zsh

```bash
# Enable completion system (if not already)
echo "autoload -U compinit; compinit" >> ~/.zshrc

# Install completions
mkdir -p ~/.zsh/completion
ctxt completion zsh > ~/.zsh/completion/_ctxt
dpkms completion zsh > ~/.zsh/completion/_dpkms

# Add to fpath in ~/.zshrc
echo 'fpath=(~/.zsh/completion $fpath)' >> ~/.zshrc

# Restart shell
exec zsh
```

#### Fish

```bash
# Install completions
mkdir -p ~/.config/fish/completions
ctxt completion fish > ~/.config/fish/completions/ctxt.fish
dpkms completion fish > ~/.config/fish/completions/dpkms.fish
```

### 5. Verify Installation

```bash
# Check versions
ctxt version
dpkms version

# Check help
ctxt --help
dpkms --help
```

## Environment Variables (Optional)

Add these to your shell profile (~/.bashrc, ~/.zshrc, etc.):

```bash
# Configuration file path
export CTXT_CONFIG="$HOME/.config/contexthelp/config.yaml"

# Data directory
export CTXT_DATA_DIR="$HOME/.local/share/contexthelp"

# Default focus profile
export CTXT_PROFILE="founder"

# dPKMS worker count
export DPKMS_WORKERS=8

# OpenAI API key (if using AI features)
export OPENAI_API_KEY="your-api-key-here"
```

## Updating

### From Source

```bash
cd ctxt
git pull origin main
task clean
task build
```

### Installed Version

```bash
cd ctxt
git pull origin main
task install
```

## Uninstallation

### Remove Binaries

```bash
# If installed with task install
rm $(go env GOPATH)/bin/ctxt
rm $(go env GOPATH)/bin/dpkms

# If manually installed to /usr/local/bin
sudo rm /usr/local/bin/ctxt
sudo rm /usr/local/bin/dpkms

# If manually installed to ~/.local/bin
rm ~/.local/bin/ctxt
rm ~/.local/bin/dpkms
```

### Remove Configuration and Data (Optional)

```bash
# Remove configuration
rm -rf ~/.config/contexthelp

# Remove data (WARNING: This deletes your knowledge base!)
rm -rf ~/.local/share/contexthelp
```

### Remove Shell Completion (Optional)

```bash
# Bash
rm ~/.bash_completion.d/ctxt
rm ~/.bash_completion.d/dpkms

# Zsh
rm ~/.zsh/completion/_ctxt
rm ~/.zsh/completion/_dpkms

# Fish
rm ~/.config/fish/completions/ctxt.fish
rm ~/.config/fish/completions/dpkms.fish
```

## Troubleshooting

### Command Not Found

If you get "command not found" after installation:

```bash
# Check if binaries are executable
chmod +x bin/ctxt bin/dpkms

# Check if directory is in PATH
echo $PATH

# Add to PATH (add to ~/.bashrc or ~/.zshrc to persist)
export PATH="$PATH:$(pwd)/bin"
```

### Permission Denied

If you get "permission denied":

```bash
# Make binaries executable
chmod +x bin/ctxt bin/dpkms
```

### Build Errors

If build fails:

```bash
# Clean and rebuild
task clean
go mod tidy
task build
```

### Configuration Not Found

If configuration file is not found:

```bash
# Check current config path
ctxt config path

# Create config directory
mkdir -p ~/.config/contexthelp

# Copy example config
cp config/config.example.yaml ~/.config/contexthelp/config.yaml
```

## Platform-Specific Notes

### macOS

```bash
# If using Homebrew, install to Homebrew prefix
HOMEBREW_PREFIX=$(brew --prefix)
sudo cp bin/ctxt "$HOMEBREW_PREFIX/bin/"
sudo cp bin/dpkms "$HOMEBREW_PREFIX/bin/"
```

### Linux

```bash
# Install to system directory
sudo cp bin/ctxt /usr/local/bin/
sudo cp bin/dpkms /usr/local/bin/

# Or use ~/.local/bin for user installation
mkdir -p ~/.local/bin
cp bin/ctxt ~/.local/bin/
cp bin/dpkms ~/.local/bin/
```

### Windows

```bash
# Build Windows binaries
GOOS=windows GOARCH=amd64 go build -o bin/ctxt.exe cmd/ctxt/main.go
GOOS=windows GOARCH=amd64 go build -o bin/dpkms.exe cmd/dpkms/main.go

# Add bin directory to PATH in System Environment Variables
```

## Development Installation

For development work:

```bash
# Clone repository
git clone https://github.com/ideacrafterslabs/ctxt.git
cd ctxt

# Install development dependencies
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Build with debug symbols
go build -gcflags="all=-N -l" -o bin/ctxt cmd/ctxt/main.go
go build -gcflags="all=-N -l" -o bin/dpkms cmd/dpkms/main.go

# Run tests
task test

# Run linter
task lint
```

## Next Steps

After installation:

1. Read the [Quick Start Guide](./docs/cli-quickstart.md)
2. Set up your [configuration](./docs/ctxt/configuration.md)
3. Start using the [CLI commands](./docs/ctxt/api-cli.md)
4. Explore focus profiles in [ctxt configuration](./docs/ctxt/configuration.md)

## Getting Help

- Check the [documentation](./docs/)
- Run `ctxt --help` or `dpkms --help`
- Read the [CLI API specification](./docs/ctxt/api-cli.md)
- Visit the [GitHub repository](https://github.com/ideacrafterslabs/ctxt)
