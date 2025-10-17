#!/bin/bash

# test.sh - Run tests with coverage reporting
# Usage: ./scripts/test.sh [options]
#   -v, --verbose    Verbose output
#   -c, --coverage   Generate coverage report
#   -h, --html       Generate HTML coverage report
#   -a, --all        Run all tests including integration tests
#   --help           Show this help message

set -e

# Default options
VERBOSE=""
COVERAGE=""
HTML=""
SHORT="-short"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -v|--verbose)
      VERBOSE="-v"
      shift
      ;;
    -c|--coverage)
      COVERAGE="true"
      shift
      ;;
    -h|--html)
      HTML="true"
      COVERAGE="true"
      shift
      ;;
    -a|--all)
      SHORT=""
      shift
      ;;
    --help)
      echo "Usage: $0 [options]"
      echo "Options:"
      echo "  -v, --verbose    Verbose output"
      echo "  -c, --coverage   Generate coverage report"
      echo "  -h, --html       Generate HTML coverage report"
      echo "  -a, --all        Run all tests including integration tests"
      echo "  --help           Show this help message"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      echo "Use --help for usage information"
      exit 1
      ;;
  esac
done

# Change to project root
cd "$(dirname "$0")/.."

echo "Running tests..."
echo "=================="

if [ -n "$COVERAGE" ]; then
  # Run tests with coverage
  echo "Generating coverage report..."
  go test $VERBOSE $SHORT -coverprofile=coverage.out -covermode=atomic ./...

  # Show coverage summary
  echo ""
  echo "Coverage Summary:"
  echo "=================="
  go tool cover -func=coverage.out | tail -n 1

  # Show coverage by package
  echo ""
  echo "Coverage by Package:"
  echo "=================="
  go tool cover -func=coverage.out | grep -v "total:" | awk '{print $1 " " $3}' | column -t

  if [ -n "$HTML" ]; then
    # Generate HTML coverage report
    echo ""
    echo "Generating HTML coverage report..."
    go tool cover -html=coverage.out -o coverage.html
    echo "HTML coverage report generated: coverage.html"

    # Try to open in browser (macOS)
    if command -v open &> /dev/null; then
      open coverage.html
    fi
  fi
else
  # Run tests without coverage
  go test $VERBOSE $SHORT ./...
fi

echo ""
echo "=================="
echo "Tests completed successfully!"
