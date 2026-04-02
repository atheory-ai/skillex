---
name: Button Accessibility
description: ARIA labels, keyboard navigation, and focus management guidelines for Button components.
---

# Button Accessibility

Make Button components accessible with proper ARIA attributes and keyboard support.

## ARIA labels

Always provide an accessible label when the button has no visible text:

```tsx
<Button aria-label="Close dialog" icon={<XIcon />} />
```

## Keyboard navigation

Buttons must be reachable via Tab and activated with Enter or Space.
Never suppress the default `onKeyDown` handler unless you re-implement it.

## Focus management

After a dialog closes, return focus to the trigger button:

```tsx
triggerRef.current?.focus();
```
