export const READ_METHODS = ["GET", "HEAD"];

// Playwright 1.65.1's own words for the two tolerated lost reads: the document
// navigated away, or the session was torn down. No other unreadable body passes.
export const navigatedAway =
  "Response body is not available for a response that was navigated away from. Read response.body() before triggering any navigation.";
export const sessionClosed = "Target page, context or browser has been closed";

export function isLostReadSentence(unreadable) {
  return unreadable.endsWith(navigatedAway) || unreadable.endsWith(sessionClosed);
}

export function unreadableReadFailure(entry) {
  const method = entry.request?.method();
  if (!READ_METHODS.includes(method)) {
    return `an unreadable response must belong to a read: ${method} ${entry.path}: ${entry.unreadable}`;
  }
  if (entry.request?.failure() || isLostReadSentence(entry.unreadable)) return null;
  return `response ${entry.path} must be readable unless its request was aborted: ${entry.unreadable}`;
}

const settleDeadlineMs = 30_000;

function withinDeadline(work, ms, refusal) {
  let timer;
  return Promise.race([
    work,
    new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(refusal)), ms);
    })
  ]).finally(() => clearTimeout(timer));
}

// The recorder's reads do not survive the navigation that replaces them, and a
// page that keeps answering would spin this loop: settle before goto, by deadline.
export async function navigate(page, recorded, url, { deadlineMs = settleDeadlineMs } = {}) {
  const deadline = Date.now() + deadlineMs;
  const refusal = `the recorder's reads did not settle within ${deadlineMs}ms before navigating to ${url}`;
  let pending = recorded.responses.length;
  for (;;) {
    if (pending > 0) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) throw new Error(refusal);
      await withinDeadline(Promise.all(recorded.responses.slice(0, pending)), remaining, refusal);
    }
    if (recorded.responses.length <= pending) break;
    pending = recorded.responses.length;
  }
  await page.goto(url);
}
