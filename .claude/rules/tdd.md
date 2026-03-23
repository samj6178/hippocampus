---
description: TDD workflow — loaded only when working with test files
paths:
  - "**/*_test.go"
---

# TDD Workflow

## Process
1. Describe testing approaches for this task/stack
2. Propose strategy options with pros/cons
3. For each option, suggest minimal test set to start
4. Check for blind spots — ask questions to find weak points
5. **Don't write code immediately** — only after approval

## During Implementation
- Table-driven tests, descriptive subtest names
- Step-by-step: test generation -> minimal impl -> full test matrix -> refactor
- Each new test/iteration is separate and deliberate
- Compare with TDD best practices, flag deviations

## Quality Bar
- Test behavior, not implementation
- Each test should fail for exactly one reason
- If setup > 5 lines AND duplicated across tests, extract a helper. Complex scenarios naturally need more setup.
- Flaky tests are worse than no tests
