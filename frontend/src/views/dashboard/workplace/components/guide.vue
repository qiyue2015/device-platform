<template>
  <div class="guide">
    <slot />
  </div>
</template>

<style lang="less" scoped>
  .guide {
    display: flex;
    min-width: 1000px;
    overflow: hidden;
    border-radius: 8px;
    opacity: 1;
    transition: all 0.2s linear;
  }
</style>
