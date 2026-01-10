import React, { useState, useCallback } from 'react';
import {
  Box,
  Stepper,
  Step,
  StepLabel,
  StepContent,
  Button,
  Typography,
  Card,
  CardContent,
  Grid,
  Slider,
  TextField,
  FormControl,
  FormControlLabel,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  Chip,
  Alert,
  AlertTitle,
  CircularProgress,
  Divider,
  Paper,
  Tooltip,
  IconButton,
  Collapse,
} from '@mui/material';
import {
  TrendingUp,
  TrendingDown,
  ShowChart,
  Timeline,
  Security,
  Speed,
  AccountBalance,
  Warning,
  CheckCircle,
  Info,
  ExpandMore,
  ExpandLess,
  Refresh,
  Save,
  PlayArrow,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

// 策略类型定义
interface StrategyConfig {
  type: string;
  name: string;
  direction: 'LONG' | 'SHORT' | 'BOTH';
  parameters: Record<string, any>;
}

// 风险评估结果
interface RiskAssessment {
  overallScore: number;
  riskLevel: 'low' | 'medium' | 'high' | 'extreme';
  scoreBreakdown: {
    capitalManagement: number;
    riskControl: number;
    strategyFit: number;
    marketCondition: number;
  };
  warnings: string[];
  suggestions: Array<{
    title: string;
    description: string;
    priority: 'high' | 'medium' | 'low';
  }>;
  recommended: boolean;
}

// 预设模板
const STRATEGY_TEMPLATES = {
  conservative: {
    name: '保守型',
    description: '低风险，稳健收益，适合新手',
    icon: <Security color="success" />,
    color: 'success.main',
    defaults: {
      maxLayers: 10,
      stopLoss: 5,
      takeProfit: 2,
      leverage: 3,
      priceStep: 2,
      multiplier: 1.2,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  balanced: {
    name: '平衡型',
    description: '中等风险，适中收益，适合有经验的交易者',
    icon: <ShowChart color="warning" />,
    color: 'warning.main',
    defaults: {
      maxLayers: 20,
      stopLoss: 10,
      takeProfit: 3,
      leverage: 5,
      priceStep: 1.5,
      multiplier: 1.5,
      trendFilter: true,
      cascadeProtection: true,
    },
  },
  aggressive: {
    name: '激进型',
    description: '高风险，追求高收益，适合专业交易者',
    icon: <Speed color="error" />,
    color: 'error.main',
    defaults: {
      maxLayers: 30,
      stopLoss: 15,
      takeProfit: 5,
      leverage: 10,
      priceStep: 1,
      multiplier: 2,
      trendFilter: false,
      cascadeProtection: true,
    },
  },
};

// 策略类型
const STRATEGY_TYPES = [
  {
    type: 'dca',
    name: '增强型 DCA',
    description: 'ATR动态间距、三重止盈、多层仓位管理',
    icon: <Timeline />,
    features: ['动态间距', '三重止盈', '瀑布保护'],
  },
  {
    type: 'martingale',
    name: '马丁格尔',
    description: '加倍加仓、风险递减控制',
    icon: <TrendingDown />,
    features: ['加倍加仓', '风险递减', '反向马丁'],
  },
  {
    type: 'combo',
    name: '组合策略',
    description: '多空对冲、市况自适应',
    icon: <AccountBalance />,
    features: ['多空对冲', '自适应权重', '全时况覆盖'],
  },
  {
    type: 'trend',
    name: '趋势跟踪',
    description: '顺势交易、动态止盈',
    icon: <TrendingUp />,
    features: ['趋势识别', '动态止盈', '移动止损'],
  },
];

interface StrategyWizardProps {
  onComplete: (config: StrategyConfig) => void;
  onCancel: () => void;
  initialConfig?: StrategyConfig;
}

const StrategyWizard: React.FC<StrategyWizardProps> = ({
  onComplete,
  onCancel,
  initialConfig,
}) => {
  const { t } = useTranslation();
  const [activeStep, setActiveStep] = useState(0);
  const [template, setTemplate] = useState<string>('balanced');
  const [strategyType, setStrategyType] = useState<string>('dca');
  const [direction, setDirection] = useState<'LONG' | 'SHORT' | 'BOTH'>('LONG');
  const [symbol, setSymbol] = useState<string>('BTCUSDT');
  const [capital, setCapital] = useState<number>(1000);
  const [parameters, setParameters] = useState<Record<string, any>>(
    STRATEGY_TEMPLATES.balanced.defaults
  );
  const [riskAssessment, setRiskAssessment] = useState<RiskAssessment | null>(null);
  const [isAssessing, setIsAssessing] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // 步骤定义
  const steps = [
    { label: '选择风格', description: '选择您的交易风格' },
    { label: '选择策略', description: '选择策略类型' },
    { label: '配置参数', description: '调整策略参数' },
    { label: '风险评估', description: '查看风险评估结果' },
    { label: '确认启动', description: '确认并启动策略' },
  ];

  // 处理模板选择
  const handleTemplateSelect = useCallback((templateKey: string) => {
    setTemplate(templateKey);
    const templateConfig = STRATEGY_TEMPLATES[templateKey as keyof typeof STRATEGY_TEMPLATES];
    setParameters(templateConfig.defaults);
  }, []);

  // 处理参数变化
  const handleParamChange = useCallback((key: string, value: any) => {
    setParameters((prev) => ({ ...prev, [key]: value }));
  }, []);

  // 执行风险评估
  const runRiskAssessment = useCallback(async () => {
    setIsAssessing(true);
    try {
      // 模拟 API 调用
      await new Promise((resolve) => setTimeout(resolve, 1500));
      
      // 计算风险评分
      let score = 100;
      const warnings: string[] = [];
      const suggestions: RiskAssessment['suggestions'] = [];

      // 评估杠杆
      if (parameters.leverage > 10) {
        score -= 20;
        warnings.push('高杠杆风险：建议降低杠杆倍数');
      } else if (parameters.leverage > 5) {
        score -= 10;
      }

      // 评估止损
      if (!parameters.stopLoss || parameters.stopLoss <= 0) {
        score -= 25;
        warnings.push('未设置止损：强烈建议设置止损保护');
        suggestions.push({
          title: '添加止损设置',
          description: '建议设置5-15%的止损比例',
          priority: 'high',
        });
      } else if (parameters.stopLoss > 20) {
        score -= 10;
        suggestions.push({
          title: '调整止损范围',
          description: '当前止损范围过大，建议收紧',
          priority: 'medium',
        });
      }

      // 评估最大层数
      if (parameters.maxLayers > 30) {
        score -= 15;
        warnings.push('最大层数过多：可能导致仓位失控');
      }

      // 评估趋势过滤
      if (!parameters.trendFilter) {
        score -= 5;
        suggestions.push({
          title: '启用趋势过滤',
          description: '在下跌趋势中暂停买入，减少风险',
          priority: 'medium',
        });
      }

      // 确保评分在有效范围内
      score = Math.max(0, Math.min(100, score));

      const riskLevel: RiskAssessment['riskLevel'] = 
        score >= 80 ? 'low' :
        score >= 60 ? 'medium' :
        score >= 40 ? 'high' : 'extreme';

      setRiskAssessment({
        overallScore: score,
        riskLevel,
        scoreBreakdown: {
          capitalManagement: Math.min(25, Math.round(score * 0.25)),
          riskControl: Math.min(25, Math.round(score * 0.25)),
          strategyFit: Math.min(25, Math.round(score * 0.25)),
          marketCondition: Math.min(25, Math.round(score * 0.25)),
        },
        warnings,
        suggestions,
        recommended: score >= 60,
      });
    } catch (error) {
      console.error('Risk assessment failed:', error);
    } finally {
      setIsAssessing(false);
    }
  }, [parameters]);

  // 下一步
  const handleNext = useCallback(() => {
    if (activeStep === 3 && !riskAssessment) {
      runRiskAssessment();
    }
    setActiveStep((prev) => prev + 1);
  }, [activeStep, riskAssessment, runRiskAssessment]);

  // 上一步
  const handleBack = useCallback(() => {
    setActiveStep((prev) => prev - 1);
  }, []);

  // 完成配置
  const handleComplete = useCallback(() => {
    const config: StrategyConfig = {
      type: strategyType,
      name: `${STRATEGY_TYPES.find(s => s.type === strategyType)?.name}_${symbol}`,
      direction,
      parameters: {
        symbol,
        capital,
        ...parameters,
      },
    };
    onComplete(config);
  }, [strategyType, symbol, direction, capital, parameters, onComplete]);

  // 获取风险等级颜色
  const getRiskColor = (level: string) => {
    switch (level) {
      case 'low': return 'success';
      case 'medium': return 'warning';
      case 'high': return 'error';
      case 'extreme': return 'error';
      default: return 'default';
    }
  };

  // 渲染步骤内容
  const renderStepContent = (step: number) => {
    switch (step) {
      case 0:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              选择您的交易风格
            </Typography>
            <Grid container spacing={2}>
              {Object.entries(STRATEGY_TEMPLATES).map(([key, value]) => (
                <Grid item xs={12} md={4} key={key}>
                  <Card
                    sx={{
                      cursor: 'pointer',
                      border: template === key ? 2 : 1,
                      borderColor: template === key ? value.color : 'divider',
                      transition: 'all 0.3s',
                      '&:hover': {
                        transform: 'translateY(-4px)',
                        boxShadow: 3,
                      },
                    }}
                    onClick={() => handleTemplateSelect(key)}
                  >
                    <CardContent>
                      <Box display="flex" alignItems="center" mb={1}>
                        {value.icon}
                        <Typography variant="h6" ml={1}>
                          {value.name}
                        </Typography>
                      </Box>
                      <Typography variant="body2" color="text.secondary">
                        {value.description}
                      </Typography>
                      {template === key && (
                        <Chip
                          label="已选择"
                          size="small"
                          color="primary"
                          sx={{ mt: 1 }}
                        />
                      )}
                    </CardContent>
                  </Card>
                </Grid>
              ))}
            </Grid>
          </Box>
        );

      case 1:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              选择策略类型
            </Typography>
            <Grid container spacing={2}>
              {STRATEGY_TYPES.map((strategy) => (
                <Grid item xs={12} md={6} key={strategy.type}>
                  <Card
                    sx={{
                      cursor: 'pointer',
                      border: strategyType === strategy.type ? 2 : 1,
                      borderColor: strategyType === strategy.type ? 'primary.main' : 'divider',
                      transition: 'all 0.3s',
                      '&:hover': {
                        transform: 'translateY(-4px)',
                        boxShadow: 3,
                      },
                    }}
                    onClick={() => setStrategyType(strategy.type)}
                  >
                    <CardContent>
                      <Box display="flex" alignItems="center" mb={1}>
                        {strategy.icon}
                        <Typography variant="h6" ml={1}>
                          {strategy.name}
                        </Typography>
                      </Box>
                      <Typography variant="body2" color="text.secondary" mb={2}>
                        {strategy.description}
                      </Typography>
                      <Box display="flex" gap={1} flexWrap="wrap">
                        {strategy.features.map((feature) => (
                          <Chip
                            key={feature}
                            label={feature}
                            size="small"
                            variant="outlined"
                          />
                        ))}
                      </Box>
                    </CardContent>
                  </Card>
                </Grid>
              ))}
            </Grid>

            <Box mt={3}>
              <Grid container spacing={2}>
                <Grid item xs={12} md={6}>
                  <FormControl fullWidth>
                    <InputLabel>交易对</InputLabel>
                    <Select
                      value={symbol}
                      label="交易对"
                      onChange={(e) => setSymbol(e.target.value)}
                    >
                      <MenuItem value="BTCUSDT">BTC/USDT</MenuItem>
                      <MenuItem value="ETHUSDT">ETH/USDT</MenuItem>
                      <MenuItem value="BNBUSDT">BNB/USDT</MenuItem>
                      <MenuItem value="SOLUSDT">SOL/USDT</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
                <Grid item xs={12} md={6}>
                  <FormControl fullWidth>
                    <InputLabel>交易方向</InputLabel>
                    <Select
                      value={direction}
                      label="交易方向"
                      onChange={(e) => setDirection(e.target.value as any)}
                    >
                      <MenuItem value="LONG">只做多</MenuItem>
                      <MenuItem value="SHORT">只做空</MenuItem>
                      <MenuItem value="BOTH">多空双向</MenuItem>
                    </Select>
                  </FormControl>
                </Grid>
              </Grid>
            </Box>
          </Box>
        );

      case 2:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              配置策略参数
            </Typography>
            
            <Paper sx={{ p: 3, mb: 3 }}>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                资金配置
              </Typography>
              <Grid container spacing={3}>
                <Grid item xs={12} md={6}>
                  <TextField
                    fullWidth
                    label="投入资金 (USDT)"
                    type="number"
                    value={capital}
                    onChange={(e) => setCapital(Number(e.target.value))}
                    InputProps={{ inputProps: { min: 100 } }}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>杠杆倍数: {parameters.leverage}x</Typography>
                  <Slider
                    value={parameters.leverage}
                    onChange={(_, v) => handleParamChange('leverage', v)}
                    min={1}
                    max={20}
                    marks={[
                      { value: 1, label: '1x' },
                      { value: 5, label: '5x' },
                      { value: 10, label: '10x' },
                      { value: 20, label: '20x' },
                    ]}
                  />
                </Grid>
              </Grid>
            </Paper>

            <Paper sx={{ p: 3, mb: 3 }}>
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                风险控制
              </Typography>
              <Grid container spacing={3}>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>止损比例: {parameters.stopLoss}%</Typography>
                  <Slider
                    value={parameters.stopLoss}
                    onChange={(_, v) => handleParamChange('stopLoss', v)}
                    min={1}
                    max={30}
                    marks={[
                      { value: 5, label: '5%' },
                      { value: 15, label: '15%' },
                      { value: 30, label: '30%' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>止盈比例: {parameters.takeProfit}%</Typography>
                  <Slider
                    value={parameters.takeProfit}
                    onChange={(_, v) => handleParamChange('takeProfit', v)}
                    min={0.5}
                    max={10}
                    step={0.5}
                    marks={[
                      { value: 1, label: '1%' },
                      { value: 5, label: '5%' },
                      { value: 10, label: '10%' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>最大层数: {parameters.maxLayers}</Typography>
                  <Slider
                    value={parameters.maxLayers}
                    onChange={(_, v) => handleParamChange('maxLayers', v)}
                    min={5}
                    max={50}
                    marks={[
                      { value: 10, label: '10' },
                      { value: 30, label: '30' },
                      { value: 50, label: '50' },
                    ]}
                  />
                </Grid>
                <Grid item xs={12} md={6}>
                  <Typography gutterBottom>价格间距: {parameters.priceStep}%</Typography>
                  <Slider
                    value={parameters.priceStep}
                    onChange={(_, v) => handleParamChange('priceStep', v)}
                    min={0.5}
                    max={5}
                    step={0.1}
                    marks={[
                      { value: 1, label: '1%' },
                      { value: 2, label: '2%' },
                      { value: 5, label: '5%' },
                    ]}
                  />
                </Grid>
              </Grid>
            </Paper>

            <Paper sx={{ p: 3 }}>
              <Box display="flex" justifyContent="space-between" alignItems="center">
                <Typography variant="subtitle1" fontWeight="bold">
                  高级设置
                </Typography>
                <IconButton onClick={() => setShowAdvanced(!showAdvanced)}>
                  {showAdvanced ? <ExpandLess /> : <ExpandMore />}
                </IconButton>
              </Box>
              <Collapse in={showAdvanced}>
                <Grid container spacing={2} mt={1}>
                  <Grid item xs={12} md={6}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={parameters.trendFilter}
                          onChange={(e) => handleParamChange('trendFilter', e.target.checked)}
                        />
                      }
                      label="趋势过滤（下跌时暂停买入）"
                    />
                  </Grid>
                  <Grid item xs={12} md={6}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={parameters.cascadeProtection}
                          onChange={(e) => handleParamChange('cascadeProtection', e.target.checked)}
                        />
                      }
                      label="瀑布保护（极端下跌时暂停）"
                    />
                  </Grid>
                  {strategyType === 'martingale' && (
                    <Grid item xs={12} md={6}>
                      <Typography gutterBottom>
                        加仓倍数: {parameters.multiplier}x
                      </Typography>
                      <Slider
                        value={parameters.multiplier}
                        onChange={(_, v) => handleParamChange('multiplier', v)}
                        min={1}
                        max={3}
                        step={0.1}
                        marks={[
                          { value: 1, label: '1x' },
                          { value: 2, label: '2x' },
                          { value: 3, label: '3x' },
                        ]}
                      />
                    </Grid>
                  )}
                </Grid>
              </Collapse>
            </Paper>
          </Box>
        );

      case 3:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              AI 风险评估
            </Typography>
            
            {isAssessing ? (
              <Box display="flex" flexDirection="column" alignItems="center" py={4}>
                <CircularProgress size={60} />
                <Typography mt={2}>正在分析您的策略配置...</Typography>
              </Box>
            ) : riskAssessment ? (
              <Box>
                <Paper sx={{ p: 3, mb: 3, textAlign: 'center' }}>
                  <Typography variant="h2" fontWeight="bold">
                    {riskAssessment.overallScore}
                  </Typography>
                  <Chip
                    label={
                      riskAssessment.riskLevel === 'low' ? '低风险' :
                      riskAssessment.riskLevel === 'medium' ? '中等风险' :
                      riskAssessment.riskLevel === 'high' ? '高风险' : '极高风险'
                    }
                    color={getRiskColor(riskAssessment.riskLevel) as any}
                    sx={{ mt: 1 }}
                  />
                </Paper>

                <Grid container spacing={2} mb={3}>
                  {Object.entries(riskAssessment.scoreBreakdown).map(([key, value]) => (
                    <Grid item xs={6} md={3} key={key}>
                      <Paper sx={{ p: 2, textAlign: 'center' }}>
                        <Typography variant="h4">{value}/25</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {key === 'capitalManagement' && '资金管理'}
                          {key === 'riskControl' && '风险控制'}
                          {key === 'strategyFit' && '策略适配'}
                          {key === 'marketCondition' && '市场条件'}
                        </Typography>
                      </Paper>
                    </Grid>
                  ))}
                </Grid>

                {riskAssessment.warnings.length > 0 && (
                  <Alert severity="warning" sx={{ mb: 2 }}>
                    <AlertTitle>警告</AlertTitle>
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {riskAssessment.warnings.map((warning, i) => (
                        <li key={i}>{warning}</li>
                      ))}
                    </ul>
                  </Alert>
                )}

                {riskAssessment.suggestions.length > 0 && (
                  <Alert severity="info" sx={{ mb: 2 }}>
                    <AlertTitle>优化建议</AlertTitle>
                    <ul style={{ margin: 0, paddingLeft: 20 }}>
                      {riskAssessment.suggestions.map((suggestion, i) => (
                        <li key={i}>
                          <strong>{suggestion.title}</strong>: {suggestion.description}
                        </li>
                      ))}
                    </ul>
                  </Alert>
                )}

                <Box display="flex" justifyContent="center" mt={2}>
                  <Button
                    variant="outlined"
                    startIcon={<Refresh />}
                    onClick={runRiskAssessment}
                  >
                    重新评估
                  </Button>
                </Box>
              </Box>
            ) : (
              <Box textAlign="center" py={4}>
                <Button
                  variant="contained"
                  size="large"
                  onClick={runRiskAssessment}
                >
                  开始风险评估
                </Button>
              </Box>
            )}
          </Box>
        );

      case 4:
        return (
          <Box>
            <Typography variant="h6" gutterBottom>
              确认策略配置
            </Typography>
            
            <Paper sx={{ p: 3, mb: 3 }}>
              <Grid container spacing={2}>
                <Grid item xs={6}>
                  <Typography color="text.secondary">策略类型</Typography>
                  <Typography variant="h6">
                    {STRATEGY_TYPES.find(s => s.type === strategyType)?.name}
                  </Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">交易对</Typography>
                  <Typography variant="h6">{symbol}</Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">交易方向</Typography>
                  <Typography variant="h6">
                    {direction === 'LONG' ? '做多' : direction === 'SHORT' ? '做空' : '双向'}
                  </Typography>
                </Grid>
                <Grid item xs={6}>
                  <Typography color="text.secondary">投入资金</Typography>
                  <Typography variant="h6">{capital} USDT</Typography>
                </Grid>
              </Grid>
              
              <Divider sx={{ my: 2 }} />
              
              <Typography variant="subtitle1" gutterBottom fontWeight="bold">
                核心参数
              </Typography>
              <Grid container spacing={2}>
                <Grid item xs={4}>
                  <Typography color="text.secondary">杠杆</Typography>
                  <Typography>{parameters.leverage}x</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">止损</Typography>
                  <Typography>{parameters.stopLoss}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">止盈</Typography>
                  <Typography>{parameters.takeProfit}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">最大层数</Typography>
                  <Typography>{parameters.maxLayers}</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">价格间距</Typography>
                  <Typography>{parameters.priceStep}%</Typography>
                </Grid>
                <Grid item xs={4}>
                  <Typography color="text.secondary">趋势过滤</Typography>
                  <Typography>{parameters.trendFilter ? '启用' : '禁用'}</Typography>
                </Grid>
              </Grid>
            </Paper>

            {riskAssessment && (
              <Alert
                severity={riskAssessment.recommended ? 'success' : 'warning'}
                icon={riskAssessment.recommended ? <CheckCircle /> : <Warning />}
              >
                风险评分: {riskAssessment.overallScore}/100 - 
                {riskAssessment.recommended ? ' 建议可以启动此策略' : ' 建议先优化配置'}
              </Alert>
            )}
          </Box>
        );

      default:
        return null;
    }
  };

  return (
    <Box sx={{ maxWidth: 900, mx: 'auto', p: 3 }}>
      <Typography variant="h4" gutterBottom fontWeight="bold">
        策略配置向导
      </Typography>
      <Typography color="text.secondary" mb={3}>
        无需编程，轻松配置您的交易策略
      </Typography>

      <Stepper activeStep={activeStep} orientation="vertical">
        {steps.map((step, index) => (
          <Step key={step.label}>
            <StepLabel>
              <Typography variant="subtitle1">{step.label}</Typography>
              <Typography variant="body2" color="text.secondary">
                {step.description}
              </Typography>
            </StepLabel>
            <StepContent>
              <Box sx={{ mb: 2 }}>
                {renderStepContent(index)}
              </Box>
              <Box sx={{ mb: 2, display: 'flex', gap: 1 }}>
                <Button
                  disabled={index === 0}
                  onClick={handleBack}
                >
                  上一步
                </Button>
                {index === steps.length - 1 ? (
                  <Button
                    variant="contained"
                    onClick={handleComplete}
                    startIcon={<PlayArrow />}
                    color="success"
                  >
                    启动策略
                  </Button>
                ) : (
                  <Button
                    variant="contained"
                    onClick={handleNext}
                  >
                    下一步
                  </Button>
                )}
                <Button onClick={onCancel} color="inherit">
                  取消
                </Button>
              </Box>
            </StepContent>
          </Step>
        ))}
      </Stepper>
    </Box>
  );
};

export default StrategyWizard;
