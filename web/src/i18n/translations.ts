export type Language = 'en' | 'vi';

export interface Translations {
  common: {
    language: string;
    english: string;
    vietnamese: string;
    resetTooltip: string;
    account: string;
    all: string;
    copied: string;
    copyCode: string;
    loading: string;
    success: string;
    error: string;
  };
  header: {
    title: string;
    tagline: string;
    tabs: {
      terminal: string;
      depth: string;
      ledger: string;
      kafka: string;
      architecture: string;
      benchmark: string;
    };
  };
  terminal: {
    marketSummary: string;
    lastPrice: string;
    change24h: string;
    volume24h: string;
    high24h: string;
    low24h: string;
    engineState: string;
    latency: string;
    ordersTab: string;
    tradesTab: string;
    positionsTab: string;
  };
  orderDesk: {
    title: string;
    subtitle: string;
    sideBuy: string;
    sideSell: string;
    limitOrder: string;
    marketOrder: string;
    price: string;
    quantity: string;
    quickQty: string;
    notional: string;
    estFee: string;
    requiredPower: string;
    availableCash: string;
    availableShares: string;
    submitBuy: string;
    submitSell: string;
    overdraftGuaranteed: string;
    orderSuccess: string;
    orderFailed: string;
    errInsufficientCash: string;
    errInsufficientShares: string;
    errLotSize: string;
    errTickSize: string;
    errPriceZero: string;
    errQuantityZero: string;
  };
  orderBook: {
    title: string;
    seq: string;
    spread: string;
    priceHeader: string;
    qtyHeader: string;
    ordersHeader: string;
    depthPercent: string;
    clickToFill: string;
  };
  tradeTape: {
    title: string;
    badge: string;
    executedCount: string;
    timeHeader: string;
    priceHeader: string;
    qtyHeader: string;
    sidesHeader: string;
    noTrades: string;
  };
  ledger: {
    title: string;
    subtitle: string;
    zeroOverdraft: string;
    totalCash: string;
    availableCash: string;
    lockedCash: string;
    holdingsTitle: string;
    holdingsDesc: string;
    symbol: string;
    totalShares: string;
    availableShares: string;
    lockedShares: string;
    marketValue: string;
    activeOrdersTitle: string;
    orderId: string;
    side: string;
    price: string;
    quantity: string;
    filled: string;
    status: string;
    action: string;
    cancel: string;
    noActiveOrders: string;
    journalTitle: string;
    journalDesc: string;
    txId: string;
    entryType: string;
    account: string;
    debit: string;
    credit: string;
    description: string;
    balancedProof: string;
  };
  kafka: {
    title: string;
    commitLog: string;
    subtitle: string;
    allTopics: string;
    topicFilter: string;
    inspectJson: string;
    hideJson: string;
    copyPayload: string;
    noEvents: string;
  };
  architecture: {
    title: string;
    subtitle: string;
    badge: string;
    categories: {
      domain: string;
      application: string;
      infrastructure: string;
      cmd: string;
      proto: string;
      deployment: string;
    };
  };
  stress: {
    title: string;
    subtitle: string;
    startBot: string;
    stopBot: string;
    burst1k: string;
    bursting: string;
    currentTps: string;
    totalSent: string;
    tradesExecuted: string;
    avgLatency: string;
    p99Latency: string;
    metricsTitle: string;
  };
  invariants: {
    title: string;
    subtitle: string;
    runButton: string;
    runningButton: string;
    passedBadge: string;
    failedBadge: string;
    tests: {
      fifo: {
        name: string;
        desc: string;
      };
      zeroOverdraft: {
        name: string;
        desc: string;
      };
      stockLock: {
        name: string;
        desc: string;
      };
      doubleEntry: {
        name: string;
        desc: string;
      };
      idempotency: {
        name: string;
        desc: string;
      };
    };
  };
  footer: {
    title: string;
    features: string;
  };
}

export const translations: Record<Language, Translations> = {
  en: {
    common: {
      language: 'Language',
      english: 'English',
      vietnamese: 'Vietnamese',
      resetTooltip: 'Reset engine state and reseed market liquidity',
      account: 'Account:',
      all: 'All',
      copied: 'Copied',
      copyCode: 'Copy Code',
      loading: 'Loading...',
      success: 'Success',
      error: 'Error',
    },
    header: {
      title: 'Domain-Driven Trading',
      tagline: '',
      tabs: {
        terminal: 'Trading Terminal',
        depth: 'L2 Order Book',
        ledger: 'Ledger & Purchasing Power',
        kafka: 'Kafka Event Log',
        architecture: 'Architecture',
        benchmark: 'Stress & Invariants',
      },
    },
    terminal: {
      marketSummary: 'Market Depth & Execution Summary',
      lastPrice: 'Last Match',
      change24h: '24h Change',
      volume24h: '24h Volume',
      high24h: '24h High',
      low24h: '24h Low',
      engineState: 'Matching Engine',
      latency: 'Engine Latency',
      ordersTab: 'Open Orders',
      tradesTab: 'Trade History',
      positionsTab: 'Positions',
    },
    orderDesk: {
      title: 'Order Placement Desk',
      subtitle: 'Pre-Trade Purchasing Power & Risk Validation',
      sideBuy: 'BUY',
      sideSell: 'SELL',
      limitOrder: 'LIMIT (LO)',
      marketOrder: 'MARKET (MP)',
      price: 'Price (VND)',
      quantity: 'Quantity (Shares)',
      quickQty: 'Quick Lots',
      notional: 'Gross Notional',
      estFee: 'Est. Fee (15 bps)',
      requiredPower: 'Total Required Cash / Purchasing Power',
      availableCash: 'Available Cash',
      availableShares: 'Available Shares',
      submitBuy: 'Submit Buy Order',
      submitSell: 'Submit Sell Order',
      overdraftGuaranteed: 'Zero Overdraft Guarantee: Order cannot exceed available purchasing power.',
      orderSuccess: 'Order accepted & processed in {latency}µs',
      orderFailed: 'Order rejected by risk engine',
      errInsufficientCash: 'Insufficient purchasing power to cover order + brokerage fee',
      errInsufficientShares: 'Insufficient available stock balance for sell order',
      errLotSize: 'Quantity must be in multiples of 100 shares (HOSE standard lot)',
      errTickSize: 'Price must conform to standard tick size (50 / 500 VND)',
      errPriceZero: 'Order price must be greater than zero for Limit Orders',
      errQuantityZero: 'Order quantity must be at least 100 shares',
    },
    orderBook: {
      title: 'L2 Order Book',
      seq: 'Seq #',
      spread: 'Spread',
      priceHeader: 'PRICE (VND)',
      qtyHeader: 'QTY (SHARES)',
      ordersHeader: 'ORDERS',
      depthPercent: 'Depth %',
      clickToFill: 'Click any price level to prefill order desk',
    },
    tradeTape: {
      title: 'Matched Execution Tape',
      badge: 'Live Matches',
      executedCount: 'executed',
      timeHeader: 'TIME / ID',
      priceHeader: 'PRICE (VND)',
      qtyHeader: 'QTY',
      sidesHeader: 'MAKER / TAKER',
      noTrades: 'No trade executions yet. Place a matching Limit/Market order to trigger execution.',
    },
    ledger: {
      title: 'Account Risk & Purchasing Power Engine',
      subtitle: 'Real-time balance reservation with double-entry debit/credit consistency.',
      zeroOverdraft: 'Zero Overdraft Active',
      totalCash: 'Total Cash Balance',
      availableCash: 'Available Purchasing Power',
      lockedCash: 'Locked / Reserved Cash',
      holdingsTitle: 'Portfolio Holdings & Position Limits',
      holdingsDesc: 'Track settled securities, open sell order locks, and current mark-to-market valuations.',
      symbol: 'Symbol',
      totalShares: 'Total Shares',
      availableShares: 'Available',
      lockedShares: 'Locked (Open Sells)',
      marketValue: 'Est. Market Value',
      activeOrdersTitle: 'Active Resting Orders in Matching Queue',
      orderId: 'Order ID',
      side: 'Side',
      price: 'Price',
      quantity: 'Quantity',
      filled: 'Filled',
      status: 'Status',
      action: 'Action',
      cancel: 'Cancel',
      noActiveOrders: 'No active resting orders for this account in the matching queue.',
      journalTitle: 'Double-Entry General Ledger Journal',
      journalDesc: 'Immutable audit log of all atomic debit and credit vouchers. Balanced invariant guaranteed.',
      txId: 'Tx ID',
      entryType: 'Type',
      account: 'Account',
      debit: 'Debit',
      credit: 'Credit',
      description: 'Description',
      balancedProof: 'Balanced Double-Entry (Debits == Credits)',
    },
    kafka: {
      title: 'Watermill & Kafka Event Log Stream',
      commitLog: 'Immutable Commit Log',
      subtitle: 'Ordered event distribution with strict Symbol/Account partition keys for deterministic FIFO replay.',
      allTopics: 'All Topics',
      topicFilter: 'Topic Filter',
      inspectJson: 'Inspect JSON ▼',
      hideJson: 'Hide Payload ▲',
      copyPayload: 'Copy JSON',
      noEvents: 'No events logged yet for selected topic.',
    },
    architecture: {
      title: 'Hexagonal DDD Architecture & In-Memory Matching',
      subtitle: 'Explore the clean separation between pure domain logic, CQRS use cases, and infrastructure adapters.',
      badge: 'Go 1.22+ PoC Codebase',
      categories: {
        domain: 'Pure Domain',
        application: 'Application CQRS',
        infrastructure: 'Infrastructure',
        cmd: 'Cmd Binaries',
        proto: 'Protobuf',
        deployment: 'Deployments',
      },
    },
    stress: {
      title: 'High-Throughput In-Memory Stress Benchmark',
      subtitle: 'Simulates high-velocity order generation and measures matching engine execution latency.',
      startBot: 'Start Continuous Bot (~25 ops/s)',
      stopBot: 'Stop Continuous Bot',
      burst1k: 'Burst 1,000 Orders',
      bursting: 'Executing Burst...',
      currentTps: 'Current Throughput',
      totalSent: 'Total Orders Sent',
      tradesExecuted: 'Trades Executed',
      avgLatency: 'Avg Engine Latency',
      p99Latency: 'p99 Tail Latency',
      metricsTitle: 'Real-Time Performance Metrics',
    },
    invariants: {
      title: 'Automated Invariant Verification Suite',
      subtitle: 'Validates the 5 architectural and financial correctness invariants defined in the trading engine specification.',
      runButton: 'Run Invariant Verification',
      runningButton: 'Verifying Invariants...',
      passedBadge: 'PASSED',
      failedBadge: 'FAILED',
      tests: {
        fifo: {
          name: '1. In-Memory FIFO Price-Time Priority Guarantee',
          desc: 'Verifies that at the same price level, earlier resting orders are filled before later orders.',
        },
        zeroOverdraft: {
          name: '2. Pre-Trade Purchasing Power & Zero Overdraft Invariant',
          desc: 'Ensures an account cannot place a buy order exceeding its cash purchasing power, including fees.',
        },
        stockLock: {
          name: '3. Stock Holdings Lock & Short-Selling Prevention',
          desc: 'Validates that sell orders cannot exceed available free shares, locking shares until filled or canceled.',
        },
        doubleEntry: {
          name: '4. Double-Entry Bookkeeping Conservation: Sum(Debits) == Sum(Credits)',
          desc: 'Proves that every matched trade generates an atomic balanced voucher where cash and stock debits equal credits.',
        },
        idempotency: {
          name: '5. Pre-Trade Idempotency Deduplication Guarantee',
          desc: 'Verifies duplicate ClientOrderIDs are rejected immediately without double debiting balance or duplicate queue entry.',
        },
      },
    },
    footer: {
      title: 'Domain-Driven Trading Engine · Go 1.22 DDD Core · Hexagonal Architecture',
      features: 'Zero Overdraft · FIFO Price-Time · Watermill EDA · PostgreSQL 16',
    },
  },
  vi: {
    common: {
      language: 'Ngôn ngữ',
      english: 'Tiếng Anh',
      vietnamese: 'Tiếng Việt',
      resetTooltip: 'Khôi phục trạng thái bộ khớp lệnh và cấp lại thanh khoản',
      account: 'Tài khoản:',
      all: 'Tất cả',
      copied: 'Đã sao chép',
      copyCode: 'Sao chép mã',
      loading: 'Đang tải...',
      success: 'Thành công',
      error: 'Lỗi',
    },
    header: {
      title: 'Hệ Thống Giao Dịch DDD',
      tagline: 'Nhân Go 1.22 DDD Core',
      tabs: {
        terminal: 'Sàn Giao Dịch',
        depth: 'Sổ Lệnh Cấp 2',
        ledger: 'Sổ Cái & Sức Mua',
        kafka: 'Nhật Ký Sự Kiện Kafka',
        architecture: 'Kiến Trúc Go DDD',
        benchmark: 'Kiểm Thử & Bất Biến',
      },
    },
    terminal: {
      marketSummary: 'Tổng Quan Độ Sâu & Khớp Lệnh Thị Trường',
      lastPrice: 'Giá Khớp Gần Nhất',
      change24h: 'Thay Đổi 24h',
      volume24h: 'Khối Lượng 24h',
      high24h: 'Cao Nhất 24h',
      low24h: 'Thấp Nhất 24h',
      engineState: 'Bộ Khớp Lệnh',
      latency: 'Độ Trễ Khớp Lệnh',
      ordersTab: 'Lệnh Chờ Khớp',
      tradesTab: 'Lịch Sử Khớp',
      positionsTab: 'Vị Thế Danh Mục',
    },
    orderDesk: {
      title: 'Bàn Đặt Lệnh Giao Dịch',
      subtitle: 'Kiểm Tra Sức Mua & Quản Trị Rủi Ro Trước Khớp',
      sideBuy: 'MUA',
      sideSell: 'BÁN',
      limitOrder: 'GIỚI HẠN (LO)',
      marketOrder: 'THỊ TRƯỜNG (MP)',
      price: 'Mức Giá (VND)',
      quantity: 'Khối Lượng (CP)',
      quickQty: 'Chọn Nhanh Lô',
      notional: 'Giá Trị Lệnh Gốc',
      estFee: 'Phí Ước Tính (0.15%)',
      requiredPower: 'Tổng Tiền Cần / Sức Mua',
      availableCash: 'Tiền Mặt Khả Dụng',
      availableShares: 'Cổ Phiếu Khả Dụng',
      submitBuy: 'Gửi Lệnh MUA',
      submitSell: 'Gửi Lệnh BÁN',
      overdraftGuaranteed: 'Bảo Đảm Tuyệt Đối Không Thấu Chi: Giá trị lệnh không được vượt quá sức mua khả dụng.',
      orderSuccess: 'Lệnh đã được tiếp nhận & xử lý trong {latency}µs',
      orderFailed: 'Lệnh bị từ chối bởi hệ thống quản trị rủi ro',
      errInsufficientCash: 'Không đủ sức mua (tiền khả dụng) để thanh toán lệnh và phí môi giới',
      errInsufficientShares: 'Không đủ cổ phiếu khả dụng trong danh mục để đặt lệnh bán',
      errLotSize: 'Khối lượng phải là bội số của 100 cổ phiếu (lô chẵn chuẩn sàn HOSE)',
      errTickSize: 'Mức giá phải tuân thủ bước giá quy định (50 / 500 VND)',
      errPriceZero: 'Mức giá phải lớn hơn 0 đối với Lệnh Giới Hạn (LO)',
      errQuantityZero: 'Khối lượng đặt phải đạt tối thiểu 100 cổ phiếu',
    },
    orderBook: {
      title: 'Sổ Lệnh Cấp 2 (L2 Depth)',
      seq: 'Số Thứ Tự #',
      spread: 'Chênh Lệch Giá',
      priceHeader: 'MỨC GIÁ (VND)',
      qtyHeader: 'KHỐI LƯỢNG (CP)',
      ordersHeader: 'SỐ LỆNH',
      depthPercent: 'Tỷ Lệ Sâu %',
      clickToFill: 'Nhấp vào mức giá bất kỳ để điền nhanh vào bàn đặt lệnh',
    },
    tradeTape: {
      title: 'Nhật Ký Khớp Lệnh Thời Gian Thực',
      badge: 'Khớp Trực Tiếp',
      executedCount: 'đã khớp',
      timeHeader: 'THỜI GIAN / MÃ',
      priceHeader: 'GIÁ KHỚP (VND)',
      qtyHeader: 'KHỐI LƯỢNG',
      sidesHeader: 'BÊN ĐẶT / BÊN KHỚP',
      noTrades: 'Chưa có giao dịch khớp nào. Hãy đặt lệnh Giới Hạn hoặc Thị Trường để khớp lệnh ngay.',
    },
    ledger: {
      title: 'Hệ Thống Quản Trị Rủi Ro & Sức Mua',
      subtitle: 'Phong tỏa số dư thời gian thực với tính nhất quán kép Nợ/Có tuyệt đối.',
      zeroOverdraft: 'Bảo Đảm Không Thấu Chi Đang Bật',
      totalCash: 'Tổng Số Dư Tiền Mặt',
      availableCash: 'Sức Mua Khả Dụng',
      lockedCash: 'Tiền Phong Tỏa / Ký Quỹ',
      holdingsTitle: 'Danh Mục Cổ Phiếu & Giới Hạn Vị Thế',
      holdingsDesc: 'Theo dõi chứng khoán khả dụng, cổ phiếu đang phong tỏa chờ bán, và định giá theo thị trường.',
      symbol: 'Mã CK',
      totalShares: 'Tổng Khối Lượng',
      availableShares: 'Khả Dụng',
      lockedShares: 'Phong Tỏa Chờ Bán',
      marketValue: 'Giá Trị Thị Trường',
      activeOrdersTitle: 'Lệnh Đang Chờ Khớp Trong Sổ Lệnh',
      orderId: 'Mã Lệnh',
      side: 'Chiều Lệnh',
      price: 'Mức Giá',
      quantity: 'Khối Lượng',
      filled: 'Đã Khớp',
      status: 'Trạng Thái',
      action: 'Thao Tác',
      cancel: 'Hủy Lệnh',
      noActiveOrders: 'Không có lệnh nào đang chờ khớp cho tài khoản này.',
      journalTitle: 'Nhật Ký Bút Toán Kép Sổ Cái Chung',
      journalDesc: 'Sổ cái bất biến ghi nhận mọi bút toán Nợ/Có nguyên tử. Đảm bảo bảo toàn Tổng Nợ == Tổng Có.',
      txId: 'Mã Bút Toán',
      entryType: 'Loại',
      account: 'Tài Khoản',
      debit: 'Ghi Nợ (Debit)',
      credit: 'Ghi Có (Credit)',
      description: 'Diễn Giải Nghiệp Vụ',
      balancedProof: 'Cân Đối Bút Toán Kép (Tổng Nợ == Tổng Có)',
    },
    kafka: {
      title: 'Luồng Sự Kiện Bất Biến Kafka & Watermill',
      commitLog: 'Nhật Ký Bất Biến (Commit Log)',
      subtitle: 'Phân phối sự kiện có thứ tự với Partition Key Mã CK / Tài khoản để tái hiện xác định theo chuẩn FIFO.',
      allTopics: 'Tất Cả Chủ Đề',
      topicFilter: 'Bộ Lọc Chủ Đề',
      inspectJson: 'Xem Chi Tiết JSON ▼',
      hideJson: 'Ẩn Chi Tiết Payload ▲',
      copyPayload: 'Sao Chép JSON',
      noEvents: 'Chưa có sự kiện nào cho chủ đề được chọn.',
    },
    architecture: {
      title: 'Kiến Trúc Lục Giác DDD & Bộ Khớp Lệnh Bộ Nhớ Trong',
      subtitle: 'Khám phá sự phân tách rõ rệt giữa nghiệp vụ miền thuần túy, điều phối CQRS và các bộ điều hợp hạ tầng.',
      badge: 'Mã Nguồn PoC Go 1.22+',
      categories: {
        domain: 'Nghiệp Vụ Miền (Pure Domain)',
        application: 'Ứng Dụng (CQRS Application)',
        infrastructure: 'Hạ Tầng (Infrastructure)',
        cmd: 'Điểm Chạy (Cmd Binaries)',
        proto: 'Định Nghĩa Protobuf',
        deployment: 'Cấu Hình Triển Khai',
      },
    },
    stress: {
      title: 'Kiểm Thử Tải Cao & Đo Lường Độ Trễ Bộ Nhớ Trong',
      subtitle: 'Mô phỏng phát sinh lệnh tần suất cực cao và đo lường độ trễ thực thi của bộ khớp lệnh.',
      startBot: 'Bật Bot Tự Động (~25 lệnh/s)',
      stopBot: 'Dừng Bot Tự Động',
      burst1k: 'Bắn Xung 1.000 Lệnh Ngay',
      bursting: 'Đang Thực Thi Xung Lệnh...',
      currentTps: 'Thông Lượng Hiện Tại',
      totalSent: 'Tổng Số Lệnh Đã Gửi',
      tradesExecuted: 'Giao Dịch Đã Khớp',
      avgLatency: 'Độ Trễ Khớp Lệnh TB',
      p99Latency: 'Độ Trễ p99 Đuôi Dài',
      metricsTitle: 'Chỉ Số Hiệu Năng Thời Gian Thực',
    },
    invariants: {
      title: 'Bộ Kiểm Tra Tính Đúng Đắn Bất Biến Tự Động',
      subtitle: 'Xác minh 5 bất biến kiến trúc và tài chính cốt lõi được định nghĩa trong đặc tả kỹ thuật sàn giao dịch.',
      runButton: 'Chạy Kiểm Chứng Bất Biến',
      runningButton: 'Đang Kiểm Tra Bất Biến...',
      passedBadge: 'ĐẠT CHUẨN',
      failedBadge: 'THẤT BẠI',
      tests: {
        fifo: {
          name: '1. Bảo Đảm Ưu Tiên Giá - Thời Gian FIFO Trong Bộ Nhớ',
          desc: 'Xác minh tại cùng một mức giá, các lệnh đến trước luôn được khớp hoàn tất trước các lệnh đến sau.',
        },
        zeroOverdraft: {
          name: '2. Bất Biến Phong Tỏa Sức Mua & Tuyệt Đối Không Thấu Chi',
          desc: 'Bảo đảm tài khoản không thể đặt lệnh mua vượt quá sức mua tiền mặt thực tế bao gồm cả phí môi giới.',
        },
        stockLock: {
          name: '3. Phong Tỏa Cổ Phiếu & Ngăn Chặn Bán Khống Trái Phép',
          desc: 'Xác minh lệnh bán không vượt quá lượng cổ phiếu khả dụng và tự động phong tỏa cổ phiếu cho tới khi khớp hoặc hủy.',
        },
        doubleEntry: {
          name: '4. Bảo Toàn Bút Toán Kép Kế Toán: Tổng Nợ == Tổng Có',
          desc: 'Chứng minh mọi giao dịch khớp lệnh đều sinh bút toán nguyên tử cân đối giữa Nợ và Có cho cả tiền và chứng khoán.',
        },
        idempotency: {
          name: '5. Bảo Đảm Tính Lũy Đẳng (Idempotency) Trùng Lặp Lệnh',
          desc: 'Xác minh các ClientOrderID gửi lặp lại đều bị từ chối ngay lập tức mà không trừ tiền hai lần hay sinh trùng lệnh.',
        },
      },
    },
    footer: {
      title: 'Hệ Thống Giao Dịch DDD · Nhân Go 1.22 DDD Core · Kiến Trúc Lục Giác',
      features: 'Tuyệt Đối Không Thấu Chi · FIFO Giá-Thời Gian · Watermill EDA · PostgreSQL 16',
    },
  },
};
