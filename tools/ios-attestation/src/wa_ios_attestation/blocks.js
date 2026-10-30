/* Apple Blocks ABI: native ownership must outlive the SDK's final release. */
const pendingBlocks = new Set();

function retainedBlock(specification, onDispose) {
  if (Process.pointerSize !== 8) throw new Error("unsupported_block_abi");
  const native = (name, result, args) =>
    new NativeFunction(Module.getGlobalExportByName(name), result, args);
  const copy = native("_Block_copy", "pointer", ["pointer"]);
  const release = native("_Block_release", "void", ["pointer"]);
  const block = new ObjC.Block(specification);
  const original = block.handle.add(24).readPointer();
  const descriptor = Memory.alloc(40);
  const copyHelper = new NativeCallback(() => {}, "void", [
    "pointer",
    "pointer",
  ]);
  const record = { block, descriptor, copyHelper };
  const dispose = () => {
    setImmediate(() => {
      if (!pendingBlocks.delete(record)) return;
      try {
        onDispose();
      } finally {
        Script.unpin();
      }
    });
  };
  const disposeHelper = new NativeCallback(dispose, "void", ["pointer"]);
  record.disposeHelper = disposeHelper;

  descriptor.writeULong(0);
  descriptor.add(8).writeULong(original.add(8).readULong());
  descriptor
    .add(16)
    .writePointer(copyHelper.strip().sign("ia", descriptor.add(16)));
  descriptor
    .add(24)
    .writePointer(disposeHelper.strip().sign("ia", descriptor.add(24)));
  descriptor.add(32).writePointer(original.add(16).readPointer());
  block.handle.writePointer(
    Module.getGlobalExportByName("_NSConcreteStackBlock"),
  );
  block.handle.add(8).writeU32((1 << 25) | (1 << 30));
  block.handle.add(24).writePointer(descriptor);

  Script.pin();
  pendingBlocks.add(record);
  let handle;
  try {
    handle = copy(block.handle);
    if (handle.isNull()) throw new Error("block_copy_failed");
  } catch (error) {
    pendingBlocks.delete(record);
    Script.unpin();
    throw error;
  }
  return { handle, release: () => release(handle) };
}
