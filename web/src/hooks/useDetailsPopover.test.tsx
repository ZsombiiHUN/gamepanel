// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { useDetailsPopover } from "./useDetailsPopover";

function Popover() {
  const { ref, close } = useDetailsPopover();
  return (
    <div>
      <p>outside</p>
      <details ref={ref} open>
        <summary>Menu</summary>
        <button type="button">inside</button>
        <button type="button" onClick={close}>
          done
        </button>
      </details>
    </div>
  );
}

const details = () => screen.getByText("Menu").closest("details")!;

describe("useDetailsPopover", () => {
  it("stays open on a click inside", () => {
    render(<Popover />);
    fireEvent.pointerDown(screen.getByText("inside"));
    expect(details().open).toBe(true);
  });

  it("closes on a click outside", () => {
    render(<Popover />);
    fireEvent.pointerDown(screen.getByText("outside"));
    expect(details().open).toBe(false);
  });

  it("closes on Escape and gives focus back to the summary", () => {
    render(<Popover />);
    fireEvent.keyDown(screen.getByText("inside"), { key: "Escape" });
    expect(details().open).toBe(false);
    expect(document.activeElement).toBe(screen.getByText("Menu"));
  });

  it("closes when told to", () => {
    render(<Popover />);
    fireEvent.click(screen.getByText("done"));
    expect(details().open).toBe(false);
  });
});
