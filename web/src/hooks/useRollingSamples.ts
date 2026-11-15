import { useState } from "react";

/**
 * The last `size` values picked from `sample`, one per new sample object.
 * Built while rendering (not in an effect), so a new reading and the chart that shows it never disagree.
 */
export function useRollingSamples<S>(sample: S | null, pick: (s: S) => number, size: number): number[] {
  const [seen, setSeen] = useState<S | null>(null);
  const [values, setValues] = useState<number[]>([]);
  if (sample !== seen) {
    setSeen(sample);
    if (sample !== null) setValues((v) => [...v.slice(-(size - 1)), pick(sample)]);
  }
  return values;
}
