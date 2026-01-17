import React, { useState, useEffect } from 'react';
import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalFooter,
  ModalBody,
  ModalCloseButton,
  Button,
  Box,
  Text,
  VStack,
  HStack,
  Badge,
  useToast,
  CircularProgress,
  CircularProgressLabel,
  Icon,
  List,
  ListItem,
  Divider,
} from '@chakra-ui/react';
import { WarningIcon, CheckCircleIcon, InfoIcon } from '@chakra-ui/icons';
import {
  Radar,
  RadarChart,
  PolarGrid,
  PolarAngleAxis,
  ResponsiveContainer,
} from 'recharts';
import { getNewbieRiskCheck, applyNewbieSecurityConfig, NewbieRiskReport, NewbieRiskCheckItem } from '../services/api';
import { useTranslation } from 'react-i18next';

interface NewbieCheckModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const NewbieCheckModal: React.FC<NewbieCheckModalProps> = ({ isOpen, onClose }) => {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [applying, setApplying] = useState(false);
  const [report, setReport] = useState<NewbieRiskReport | null>(null);
  const toast = useToast();

  const fetchReport = async () => {
    setLoading(true);
    try {
      const data = await getNewbieRiskCheck();
      setReport(data);
    } catch (error) {
      console.error('获取新手体检报告失败:', error);
      toast({
        title: '获取报告失败',
        description: error instanceof Error ? error.message : '未知错误',
        status: 'error',
        duration: 5000,
        isClosable: true,
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (isOpen) {
      fetchReport();
    }
  }, [isOpen]);

  const handleApply = async () => {
    setApplying(true);
    try {
      const response = await applyNewbieSecurityConfig();
      if (response.success) {
        toast({
          title: '应用成功',
          description: response.message,
          status: 'success',
          duration: 5000,
          isClosable: true,
        });
        fetchReport(); // 重新获取报告
      }
    } catch (error) {
      toast({
        title: '应用失败',
        description: error instanceof Error ? error.message : '未知错误',
        status: 'error',
        duration: 5000,
        isClosable: true,
      });
    } finally {
      setApplying(false);
    }
  };

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'safe': return 'green.500';
      case 'warning': return 'orange.500';
      case 'danger': return 'red.500';
      default: return 'gray.500';
    }
  };

  const getLevelIcon = (level: string) => {
    switch (level) {
      case 'safe': return CheckCircleIcon;
      case 'warning': return WarningIcon;
      case 'danger': return WarningIcon;
      default: return InfoIcon;
    }
  };

  const chartData = report?.results.map(item => ({
    subject: item.item,
    A: item.score,
    fullMark: 100,
  })) || [];

  return (
    <Modal isOpen={isOpen} onClose={onClose} size="xl" scrollBehavior="inside">
      <ModalOverlay />
      <ModalContent bg="gray.800" color="white">
        <ModalHeader borderBottomWidth="1px" borderColor="gray.700">
          🛡️ 新手保护体检
        </ModalHeader>
        <ModalCloseButton />
        <ModalBody p={6}>
          {loading ? (
            <VStack py={20}>
              <CircularProgress isIndeterminate color="blue.400" size="80px" />
              <Text mt={4}>正在进行全维度风险扫描...</Text>
            </VStack>
          ) : report ? (
            <VStack spacing={6} align="stretch">
              <HStack spacing={8} justify="center" p={4} bg="gray.900" borderRadius="lg">
                <Box textAlign="center">
                  <CircularProgress 
                    value={report.overallScore} 
                    color={report.overallScore > 80 ? 'green.400' : report.overallScore > 50 ? 'orange.400' : 'red.400'} 
                    size="120px"
                    thickness="8px"
                  >
                    <CircularProgressLabel fontSize="2xl" fontWeight="bold">
                      {report.overallScore}
                    </CircularProgressLabel>
                  </CircularProgress>
                  <Text mt={2} fontSize="sm" color="gray.400">综合安全分</Text>
                </Box>
                
                <Box height="200px" width="300px">
                  <ResponsiveContainer width="100%" height="100%">
                    <RadarChart cx="50%" cy="50%" outerRadius="70%" data={chartData}>
                      <PolarGrid stroke="#4A5568" />
                      <PolarAngleAxis dataKey="subject" tick={{ fill: '#A0AEC0', fontSize: 12 }} />
                      <Radar
                        name="Score"
                        dataKey="A"
                        stroke="#4299E1"
                        fill="#4299E1"
                        fillOpacity={0.6}
                      />
                    </RadarChart>
                  </ResponsiveContainer>
                </Box>
              </HStack>

              <Box>
                <Text fontSize="lg" fontWeight="bold" mb={4}>详细体检结果</Text>
                <List spacing={4}>
                  {report.results.map((result, index) => (
                    <ListItem key={index} p={4} bg="gray.700" borderRadius="md">
                      <HStack align="start" spacing={3}>
                        <Icon as={getLevelIcon(result.level)} color={getLevelColor(result.level)} mt={1} />
                        <VStack align="start" spacing={1} flex={1}>
                          <HStack justify="space-between" width="100%">
                            <Text fontWeight="bold">{result.item}</Text>
                            <Badge colorScheme={result.level === 'safe' ? 'green' : result.level === 'warning' ? 'orange' : 'red'}>
                              得分: {result.score}
                            </Badge>
                          </HStack>
                          <Text fontSize="sm" color="gray.200">{result.message}</Text>
                          <Text fontSize="xs" color="gray.400" fontStyle="italic">💡 建议: {result.advice}</Text>
                        </VStack>
                      </HStack>
                    </ListItem>
                  ))}
                </List>
              </Box>
            </VStack>
          ) : (
            <Text>未能生成报告，请重试。</Text>
          )}
        </ModalBody>
        <ModalFooter borderTopWidth="1px" borderColor="gray.700" bg="gray.900">
          <Button variant="ghost" mr={3} onClick={onClose} _hover={{ bg: 'gray.700' }}>
            关闭
          </Button>
          <Button 
            colorScheme="blue" 
            leftIcon={<CheckCircleIcon />} 
            onClick={handleApply}
            isLoading={applying}
            loadingText="正在加固..."
          >
            一键安全加固
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
};
